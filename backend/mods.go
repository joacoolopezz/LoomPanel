package main

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	modrinthBase = "https://api.modrinth.com/v2"
	modMu        sync.Mutex
)

// allowLocalDL returns true only when explicitly enabled via env var (for testing).
// This avoids a global mutable variable that tests could accidentally leave enabled.
func allowLocalDL() bool {
	return os.Getenv("GP_ALLOW_LOCAL_DL") == "1"
}

func modDirName(typ string) string {
	switch typ {
	case "paper", "purpur":
		return "plugins"
	case "fabric", "quilt", "forge", "neoforge":
		return "mods"
	}
	return ""
}

func loadersFor(typ string) []string {
	switch typ {
	case "paper":
		return []string{"paper", "spigot", "bukkit"}
	case "purpur":
		return []string{"purpur", "paper", "spigot", "bukkit"}
	case "quilt":
		return []string{"quilt", "fabric"}
	}
	return []string{typ}
}

func jsonArr(a []string) string { b, _ := json.Marshal(a); return string(b) }

func mrGet(path string, v any) error {
	req, _ := http.NewRequest("GET", modrinthBase+path, nil)
	req.Header.Set("User-Agent", "loompanel/0.2 (self-hosted game server panel)")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("modrinth: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(v)
}

type mrFile struct {
	Hashes struct {
		SHA512 string `json:"sha512"`
	} `json:"hashes"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
}
type mrVersion struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	VersionType  string   `json:"version_type"`
	VersionNum   string   `json:"version_number"`
	Files        []mrFile `json:"files"`
	Dependencies []struct {
		ProjectID string `json:"project_id"`
		Type      string `json:"dependency_type"`
	} `json:"dependencies"`
}

func pickVersion(in Instance, projectID string) (*mrVersion, error) {
	q := url.Values{}
	q.Set("loaders", jsonArr(loadersFor(in.Type)))
	q.Set("game_versions", jsonArr([]string{in.Version}))
	var vs []mrVersion
	if err := mrGet("/project/"+url.PathEscape(projectID)+"/version?"+q.Encode(), &vs); err != nil {
		return nil, err
	}
	for i := range vs {
		if vs[i].VersionType == "release" {
			return &vs[i], nil
		}
	}
	if len(vs) > 0 {
		return &vs[0], nil
	}
	return nil, errors.New("no_compatible_version")
}

// Trusted CDN hosts for Modrinth downloads (strict allowlist)
var trustedModrinthHosts = map[string]bool{
	"cdn.modrinth.com":        true,
	"cdn.jsdelivr.net":        true, // Some projects use jsDelivr mirror
	"objects.githubusercontent.com": true, // GitHub releases
}

func mrDownload(fileURL, dest, wantSHA512 string) error {
	u, err := url.Parse(fileURL)
	if err != nil {
		return errors.New("invalid URL")
	}
	if u.Scheme != "https" {
		return errors.New("only HTTPS downloads allowed")
	}
	if !allowLocalDL() && !trustedModrinthHosts[u.Host] {
		return errors.New("untrusted download host: " + u.Host)
	}
	client := fetchClient
	if allowLocalDL() {
		client = http.DefaultClient
	}
	req, _ := http.NewRequest("GET", fileURL, nil)
	req.Header.Set("User-Agent", "loompanel/0.2")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dest + ".part")
	if err != nil {
		return err
	}
	h := sha512.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 300<<20))
	f.Close()
	if err == nil && wantSHA512 != "" && hex.EncodeToString(h.Sum(nil)) != strings.ToLower(wantSHA512) {
		err = errors.New("checksum mismatch")
	}
	if err != nil {
		os.Remove(dest + ".part")
		return err
	}
	return os.Rename(dest+".part", dest)
}

// ---- metadata: which installed jar came from which Modrinth project ----

type modEntry struct {
	ProjectID string `json:"projectId"`
	VersionID string `json:"versionId"`
	Version   string `json:"version"`
	Title     string `json:"title"`
}
type modMeta map[string]modEntry

func metaPath(id string) string { return filepath.Join(instDir(id), ".loompanel", "mods.json") }

func loadMeta(id string) modMeta {
	m := modMeta{}
	if b, err := os.ReadFile(metaPath(id)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func saveMeta(id string, m modMeta) error {
	_ = os.MkdirAll(filepath.Dir(metaPath(id)), 0o755)
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(metaPath(id), b, 0o644)
}

func installProject(in Instance, projectID string, depth int, seen map[string]bool) ([]string, error) {
	if seen[projectID] || depth > 3 {
		return nil, nil
	}
	seen[projectID] = true
	v, err := pickVersion(in, projectID)
	if err != nil {
		return nil, err
	}

	// Fast check without lock
	meta := loadMeta(in.ID)
	for _, e := range meta {
		if e.ProjectID == v.ProjectID && e.VersionID == v.ID {
			return nil, nil // already on the latest compatible version
		}
	}

	var file *mrFile
	for i := range v.Files {
		if v.Files[i].Primary || file == nil {
			file = &v.Files[i]
		}
	}
	if file == nil {
		return nil, errors.New("version has no files")
	}
	name, ok := cleanName(file.Filename)
	if !ok || !strings.HasSuffix(strings.ToLower(name), ".jar") {
		return nil, errors.New("not a jar file")
	}
	dir := filepath.Join(instDir(in.ID), modDirName(in.Type))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := mrDownload(file.URL, filepath.Join(dir, name), file.Hashes.SHA512); err != nil {
		return nil, err
	}
	title := name
	var p struct {
		Title string `json:"title"`
	}
	if mrGet("/project/"+url.PathEscape(v.ProjectID), &p) == nil && p.Title != "" {
		title = p.Title
	}

	// Re-check under lock after download to avoid duplicate installs
	modMu.Lock()
	meta = loadMeta(in.ID)
	for _, e := range meta {
		if e.ProjectID == v.ProjectID && e.VersionID == v.ID {
			modMu.Unlock()
			return nil, nil // another request installed it while we were downloading
		}
	}
	for fn, e := range meta { // replace older versions of the same project
		if e.ProjectID == v.ProjectID && fn != name {
			_ = os.Remove(filepath.Join(dir, fn))
			_ = os.Remove(filepath.Join(dir, fn+".disabled"))
			delete(meta, fn)
		}
	}
	meta[name] = modEntry{v.ProjectID, v.ID, v.VersionNum, title}
	err = saveMeta(in.ID, meta)
	modMu.Unlock()
	if err != nil {
		return nil, err
	}
	installed := []string{title}
	for _, d := range v.Dependencies {
		if d.Type == "required" && d.ProjectID != "" {
			if sub, err := installProject(in, d.ProjectID, depth+1, seen); err == nil {
				installed = append(installed, sub...)
			}
		}
	}
	return installed, nil
}

func modsGuard(w http.ResponseWriter, in Instance) bool {
	if modDirName(in.Type) == "" {
		fail(w, 400, "no_mods_support")
		return false
	}
	return true
}

func handleModsSearch(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	if !modsGuard(w, in) {
		return
	}
	var loaders []string
	for _, l := range loadersFor(in.Type) {
		loaders = append(loaders, "categories:"+l)
	}
	facets, _ := json.Marshal([][]string{loaders, {"versions:" + in.Version}})
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	q := url.Values{}
	q.Set("query", truncate(r.URL.Query().Get("q"), 80))
	q.Set("facets", string(facets))
	q.Set("limit", "20")
	q.Set("offset", strconv.Itoa(max(page, 0)*20))
	if r.URL.Query().Get("q") == "" {
		q.Set("index", "downloads")
	}
	var res struct {
		Hits []struct {
			ProjectID   string `json:"project_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			IconURL     string `json:"icon_url"`
			Downloads   int    `json:"downloads"`
			Author      string `json:"author"`
			ProjectType string `json:"project_type"`
		} `json:"hits"`
	}
	if err := mrGet("/search?"+q.Encode(), &res); err != nil {
		fail(w, 502, "upstream_unavailable")
		return
	}
	out := []map[string]any{}
	for _, h := range res.Hits {
		switch h.ProjectType {
		case "modpack", "resourcepack", "shader", "datapack":
			continue
		}
		out = append(out, map[string]any{"id": h.ProjectID, "title": h.Title, "description": h.Description, "icon": h.IconURL, "downloads": h.Downloads, "author": h.Author})
	}
	writeJSON(w, 200, out)
}

func handleModsInstall(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct {
		ProjectID string `json:"projectId"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	if !modsGuard(w, in) {
		return
	}
	if b.ProjectID == "" || len(b.ProjectID) > 40 {
		fail(w, 400, "invalid_input")
		return
	}
	installed, err := installProject(in, b.ProjectID, 0, map[string]bool{})
	if err != nil {
		code := "install_failed"
		if err.Error() == "no_compatible_version" {
			code = err.Error()
		}
		fail(w, 502, code)
		return
	}
	audit(r, u.Username, "mod.install", in.Name+": "+strings.Join(installed, ", "))
	writeJSON(w, 200, map[string]any{"installed": installed})
}

func handleModsList(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	if !modsGuard(w, in) {
		return
	}
	meta := loadMeta(in.ID)
	ents, _ := os.ReadDir(filepath.Join(instDir(in.ID), modDirName(in.Type)))
	items := []map[string]any{}
	for _, e := range ents {
		n := e.Name()
		enabled := strings.HasSuffix(n, ".jar")
		if !enabled && !strings.HasSuffix(n, ".jar.disabled") {
			continue
		}
		base := strings.TrimSuffix(n, ".disabled")
		fi, err := e.Info()
		if err != nil {
			continue
		}
		m := meta[base]
		items = append(items, map[string]any{"file": base, "enabled": enabled, "size": fi.Size(), "title": m.Title, "version": m.Version, "projectId": m.ProjectID})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i]["file"].(string)) < strings.ToLower(items[j]["file"].(string))
	})
	writeJSON(w, 200, map[string]any{"kind": modDirName(in.Type), "items": items})
}

func modFile(w http.ResponseWriter, in Instance, name string) (dir, base string, ok bool) {
	n, good := cleanName(name)
	if !good || n != name || !strings.HasSuffix(n, ".jar") {
		fail(w, 400, "invalid_input")
		return "", "", false
	}
	return filepath.Join(instDir(in.ID), modDirName(in.Type)), n, true
}

func handleModsToggle(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct {
		File    string `json:"file"`
		Enabled bool   `json:"enabled"`
	}
	if !readJSON(w, r, &b) || !modsGuard(w, in) {
		return
	}
	dir, base, ok := modFile(w, in, b.File)
	if !ok {
		return
	}
	from, to := filepath.Join(dir, base+".disabled"), filepath.Join(dir, base)
	if !b.Enabled {
		from, to = to, from
	}
	if err := os.Rename(from, to); err != nil {
		fail(w, 404, "not_found")
		return
	}
	audit(r, u.Username, "mod.toggle", in.Name+": "+base)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleModsRemove(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct {
		File string `json:"file"`
	}
	if !readJSON(w, r, &b) || !modsGuard(w, in) {
		return
	}
	dir, base, ok := modFile(w, in, b.File)
	if !ok {
		return
	}
	_ = os.Remove(filepath.Join(dir, base))
	_ = os.Remove(filepath.Join(dir, base+".disabled"))
	modMu.Lock()
	meta := loadMeta(in.ID)
	delete(meta, base)
	_ = saveMeta(in.ID, meta)
	modMu.Unlock()
	audit(r, u.Username, "mod.remove", in.Name+": "+base)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleModsUpdates(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	if !modsGuard(w, in) {
		return
	}
	out := []map[string]any{}
	n := 0
	seen := map[string]bool{}
	for _, e := range loadMeta(in.ID) {
		if n++; n > 40 || seen[e.ProjectID] {
			continue
		}
		seen[e.ProjectID] = true
		v, err := pickVersion(in, e.ProjectID)
		if err == nil && v.ID != e.VersionID {
			out = append(out, map[string]any{"projectId": e.ProjectID, "title": e.Title, "current": e.Version, "latest": v.VersionNum})
		}
	}
	writeJSON(w, 200, out)
}

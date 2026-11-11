package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) Instance {
	t.Helper()
	cfg = Config{DataDir: t.TempDir()}
	if err := openDB(cfg.DataDir); err != nil {
		t.Fatal(err)
	}
	in := Instance{ID: "t1", Name: "t", Slug: "t", Type: "fabric", Version: "1.21.4", State: "ready"}
	_ = os.MkdirAll(instDir(in.ID), 0o755)
	return in
}

func TestCron(t *testing.T) {
	at := func(s string) time.Time { x, _ := time.Parse("2006-01-02 15:04", s); return x }
	cases := []struct {
		cron, when string
		want       bool
	}{
		{"0 4 * * *", "2026-11-09 04:00", true}, {"0 4 * * *", "2026-11-09 04:01", false},
		{"*/15 * * * *", "2026-11-09 10:45", true}, {"*/15 * * * *", "2026-11-09 10:50", false},
		{"0 */6 * * *", "2026-11-09 12:00", true}, {"0 */6 * * *", "2026-11-09 13:00", false},
		{"30 2 * * 1-5", "2026-11-09 02:30", true},  // Monday
		{"30 2 * * 1-5", "2026-11-08 02:30", false}, // Sunday
		{"0 0 * * 7", "2026-11-08 00:00", true},     // 7 = Sunday
		{"0 0 1,15 * *", "2026-11-15 00:00", true},
	}
	for _, c := range cases {
		s, err := parseCron(c.cron)
		if err != nil {
			t.Fatalf("%s: %v", c.cron, err)
		}
		if got := s.match(at(c.when)); got != c.want {
			t.Errorf("%s at %s = %v, want %v", c.cron, c.when, got, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "a * * * *", "*/0 * * * *", "5-1 * * * *"} {
		if _, err := parseCron(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestBackupRestoreAndRetention(t *testing.T) {
	in := setup(t)
	dir := instDir(in.ID)
	_ = os.MkdirAll(filepath.Join(dir, "world"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "world", "level.dat"), []byte("original"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "server.properties"), []byte("motd=hi"), 0o644)
	_ = os.Symlink("/etc/passwd", filepath.Join(dir, "link"))

	id, err := createBackup(in, "manual")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "world", "level.dat"), []byte("CORRUPTED"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("x"), 0o644)

	if err := restoreBackup(in, id); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "world", "level.dat")); string(b) != "original" {
		t.Errorf("restore failed: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "junk.txt")); err == nil {
		t.Error("junk.txt should be gone after restore")
	}
	if _, err := os.Lstat(filepath.Join(dir, "link")); err == nil {
		t.Error("symlink should not be archived")
	}
	var pre int
	_ = db.QueryRow(`SELECT COUNT(*) FROM backups WHERE kind='pre-restore'`).Scan(&pre)
	if pre != 1 {
		t.Errorf("expected 1 pre-restore backup, got %d", pre)
	}
	for i := 0; i < 5; i++ {
		if _, err := createBackup(in, "scheduled"); err != nil {
			t.Fatal(err)
		}
	}
	pruneBackups(in.ID, "scheduled", 2)
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM backups WHERE kind='scheduled'`).Scan(&n)
	files, _ := os.ReadDir(backupDir(in.ID))
	if n != 2 || len(files) != 2+1+1 { // 2 scheduled + 1 manual + 1 pre-restore
		t.Errorf("retention: rows=%d files=%d", n, len(files))
	}
}

func TestExtractRejectsTraversal(t *testing.T) {
	tmp := t.TempDir()
	arc := filepath.Join(tmp, "evil.tar.gz")
	f, _ := os.Create(arc)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../escaped.txt", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("bad"))
	tw.Close()
	gz.Close()
	f.Close()
	dst := filepath.Join(tmp, "out")
	_ = os.MkdirAll(dst, 0o755)
	// "../escaped.txt" is cleaned to "escaped.txt" inside dst: it must never land outside
	_ = extractTar(arc, dst)
	if _, err := os.Stat(filepath.Join(tmp, "escaped.txt")); err == nil {
		t.Fatal("file escaped the destination")
	}
}

func fakeModrinth(t *testing.T, content map[string]string, versions map[string]string) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/files/"):
			fmt.Fprint(w, content[strings.TrimPrefix(r.URL.Path, "/files/")])
		case strings.HasSuffix(r.URL.Path, "/version"):
			pid := strings.Split(r.URL.Path, "/")[2]
			fn := versions[pid]
			h := sha512.Sum512([]byte(content[fn]))
			sum := hex.EncodeToString(h[:])
			if strings.HasPrefix(fn, "bad") {
				sum = strings.Repeat("0", 128)
			}
			deps := []map[string]string{}
			if pid == "main" {
				deps = append(deps, map[string]string{"project_id": "dep", "dependency_type": "required"}, map[string]string{"project_id": "opt", "dependency_type": "optional"})
			}
			json.NewEncoder(w).Encode([]map[string]any{{"id": "v-" + fn, "project_id": pid, "version_type": "release", "version_number": "1.0",
				"files": []map[string]any{{"url": srv.URL + "/files/" + fn, "filename": fn, "primary": true, "hashes": map[string]string{"sha512": sum}}}, "dependencies": deps}})
		default:
			json.NewEncoder(w).Encode(map[string]string{"title": "Title of " + r.URL.Path})
		}
	}))
	return srv
}

func TestModInstall(t *testing.T) {
	in := setup(t)
	allowLocalDL = true
	defer func() { allowLocalDL = false }()
	content := map[string]string{"main-1.jar": "MAIN1", "main-2.jar": "MAIN2", "dep-1.jar": "DEP", "bad-1.jar": "BAD"}
	versions := map[string]string{"main": "main-1.jar", "dep": "dep-1.jar", "evil": "bad-1.jar"}
	srv := fakeModrinth(t, content, versions)
	defer srv.Close()
	modrinthBase = srv.URL

	got, err := installProject(in, "main", 0, map[string]bool{})
	if err != nil || len(got) != 2 {
		t.Fatalf("install: %v %v", got, err)
	}
	mods := filepath.Join(instDir(in.ID), "mods")
	for _, f := range []string{"main-1.jar", "dep-1.jar"} {
		if _, err := os.Stat(filepath.Join(mods, f)); err != nil {
			t.Errorf("%s missing", f)
		}
	}
	if again, _ := installProject(in, "main", 0, map[string]bool{}); len(again) != 0 {
		t.Errorf("second install should be a no-op, got %v", again)
	}
	versions["main"] = "main-2.jar" // an update appears: old jar must be replaced
	if _, err := installProject(in, "main", 0, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mods, "main-1.jar")); err == nil {
		t.Error("old version should have been removed")
	}
	if _, err := os.Stat(filepath.Join(mods, "main-2.jar")); err != nil {
		t.Error("new version missing")
	}
	if _, err := installProject(in, "evil", 0, map[string]bool{}); err == nil {
		t.Error("checksum mismatch must fail")
	}
	if _, err := os.Stat(filepath.Join(mods, "bad-1.jar")); err == nil {
		t.Error("bad file must not remain")
	}
	if _, err := os.Stat(filepath.Join(mods, "bad-1.jar.part")); err == nil {
		t.Error("partial file must not remain")
	}
	if m := loadMeta(in.ID); len(m) != 2 || m["main-2.jar"].Title == "" {
		t.Errorf("meta: %+v", m)
	}
}

func TestDownloadHostAllowlist(t *testing.T) {
	setup(t)
	if err := mrDownload("https://evil.example.com/a.jar", filepath.Join(t.TempDir(), "a.jar"), ""); err == nil {
		t.Error("non-modrinth host must be refused")
	}
	if err := mrDownload("http://cdn.modrinth.com/a.jar", filepath.Join(t.TempDir(), "a.jar"), ""); err == nil {
		t.Error("plain http must be refused")
	}
}

func TestLoaderMapping(t *testing.T) {
	if mc, n, ok := neoMC("21.4.123"); !ok || mc != "1.21.4" || n != 123 {
		t.Errorf("neo 21.4.123 -> %s %d %v", mc, n, ok)
	}
	if mc, _, ok := neoMC("21.0.5"); !ok || mc != "1.21" {
		t.Errorf("neo 21.0.5 -> %s", mc)
	}
	if _, _, ok := neoMC("21.4.1-beta"); ok {
		t.Error("beta must be skipped")
	}
	l := []string{"1.20.1", "1.21", "1.21.4", "1.20.6"}
	sortMC(l)
	if strings.Join(l, ",") != "1.21.4,1.21,1.20.6,1.20.1" {
		t.Errorf("sort: %v", l)
	}
}

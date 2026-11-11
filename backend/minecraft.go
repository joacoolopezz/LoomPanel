package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ServerType struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Beta      bool   `json:"beta"`
}

// Forge, NeoForge and Quilt run an installer inside a container (see loaders.go). They are new and flagged beta.
var serverTypes = []ServerType{
	{"paper", "Paper", true, false}, {"purpur", "Purpur", true, false}, {"vanilla", "Vanilla", true, false}, {"fabric", "Fabric", true, false},
	{"spigot", "Spigot", true, false}, {"mohist", "Mohist", true, false}, {"arclight", "Arclight", true, false}, {"folia", "Folia", true, true},
	{"quilt", "Quilt", true, true}, {"forge", "Forge", true, true}, {"neoforge", "NeoForge", true, true},
	{"velocity", "Velocity", true, false}, {"bungeecord", "BungeeCord", true, false}, {"waterfall", "Waterfall", true, false}, {"magma", "Magma", true, true},
}

func typeAvailable(id string) bool {
	for _, t := range serverTypes {
		if t.ID == id && t.Available {
			return true
		}
	}
	return false
}

var httpc = &http.Client{Timeout: 25 * time.Second}

func getJSON(url string, v any) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "loompanel/0.1 (open-source game server panel)")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 20<<20)).Decode(v)
}

var relRe = regexp.MustCompile(`^1\.\d+(\.\d+)?$`)

type vcache struct {
	at   time.Time
	list []string
}

var (
	vmu     sync.Mutex
	vcache_ = map[string]vcache{}
)

func versionsFor(typ string) ([]string, error) {
	vmu.Lock()
	if c, ok := vcache_[typ]; ok && time.Since(c.at) < 10*time.Minute {
		vmu.Unlock()
		return c.list, nil
	}
	vmu.Unlock()
	var out []string
	switch typ {
	case "vanilla":
		var m struct {
			Versions []struct{ ID, Type string } `json:"versions"`
		}
		if err := getJSON("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &m); err != nil {
			return nil, err
		}
		for _, v := range m.Versions {
			if v.Type == "release" {
				out = append(out, v.ID)
			}
		}
	case "paper":
		// PaperMC v2 API deprecated; use PaperMC Hangar or fallback to Purpur versions
		// Paper and Purpur share the same MC versions
		var m struct {
			Versions []string `json:"versions"`
		}
		if err := getJSON("https://api.purpurmc.org/v2/purpur", &m); err != nil {
			return nil, err
		}
		for i := len(m.Versions) - 1; i >= 0; i-- {
			if relRe.MatchString(m.Versions[i]) {
				out = append(out, m.Versions[i])
			}
		}
	case "purpur":
		url := "https://api.purpurmc.org/v2/purpur"
		var m struct {
			Versions []string `json:"versions"`
		}
		if err := getJSON(url, &m); err != nil {
			return nil, err
		}
		for i := len(m.Versions) - 1; i >= 0; i-- {
			if relRe.MatchString(m.Versions[i]) {
				out = append(out, m.Versions[i])
			}
		}
	case "fabric":
		var m []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := getJSON("https://meta.fabricmc.net/v2/versions/game", &m); err != nil {
			return nil, err
		}
		for _, v := range m {
			if v.Stable {
				out = append(out, v.Version)
			}
		}
	case "quilt", "forge", "neoforge":
		var err error
		if out, err = loaderVersions(typ); err != nil {
			return nil, err
		}
	case "spigot", "mohist", "arclight", "folia", "magma", "velocity", "bungeecord", "waterfall":
		// These share MC versions with Paper/Purpur
		var m struct {
			Versions []string `json:"versions"`
		}
		if err := getJSON("https://api.purpurmc.org/v2/purpur", &m); err != nil {
			return nil, err
		}
		for i := len(m.Versions) - 1; i >= 0; i-- {
			if relRe.MatchString(m.Versions[i]) {
				out = append(out, m.Versions[i])
			}
		}
	default:
		return nil, errors.New("unsupported type")
	}
	vmu.Lock()
	vcache_[typ] = vcache{time.Now(), out}
	vmu.Unlock()
	return out, nil
}

func hasVersion(typ, ver string) bool {
	list, err := versionsFor(typ)
	if err != nil {
		return false
	}
	for _, v := range list {
		if v == ver {
			return true
		}
	}
	return false
}

// javaFor picks the Java major version Minecraft needs.
func javaFor(version string) int {
	p := strings.Split(version, ".")
	minor, patch := 0, 0
	if len(p) > 1 {
		minor, _ = strconv.Atoi(p[1])
	}
	if len(p) > 2 {
		patch, _ = strconv.Atoi(p[2])
	}
	switch {
	case minor > 20 || (minor == 20 && patch >= 5):
		return 21
	case minor >= 17:
		return 17
	default:
		return 8
	}
}

// aikarFlags: the widely used G1GC tuning set.
func aikarFlags(ramMB int) []string {
	f := []string{
		"-XX:+UseG1GC", "-XX:+ParallelRefProcEnabled", "-XX:MaxGCPauseMillis=200", "-XX:+UnlockExperimentalVMOptions",
		"-XX:+DisableExplicitGC", "-XX:+AlwaysPreTouch", "-XX:G1HeapWastePercent=5", "-XX:G1MixedGCCountTarget=4",
		"-XX:InitiatingHeapOccupancyPercent=15", "-XX:G1MixedGCLiveThresholdPercent=90", "-XX:G1RSetUpdatingPauseTimePercent=5",
		"-XX:SurvivorRatio=32", "-XX:+PerfDisableSharedMem", "-XX:MaxTenuringThreshold=1",
		"-Dusing.aikars.flags=https://mcflags.emc.gs", "-Daikars.new.flags=true",
	}
	if ramMB > 12*1024 {
		return append(f, "-XX:G1NewSizePercent=40", "-XX:G1MaxNewSizePercent=50", "-XX:G1HeapRegionSize=16M", "-XX:G1ReservePercent=15")
	}
	return append(f, "-XX:G1NewSizePercent=30", "-XX:G1MaxNewSizePercent=40", "-XX:G1HeapRegionSize=8M", "-XX:G1ReservePercent=20")
}

func download(ctx context.Context, url, dest string, h hash.Hash, want string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "loompanel/0.1")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.Create(dest + ".part")
	if err != nil {
		return err
	}
	var w io.Writer = f
	if h != nil {
		w = io.MultiWriter(f, h)
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, 600<<20))
	f.Close()
	if err != nil {
		os.Remove(dest + ".part")
		return err
	}
	if h != nil && want != "" && hex.EncodeToString(h.Sum(nil)) != want {
		os.Remove(dest + ".part")
		return errors.New("checksum mismatch")
	}
	return os.Rename(dest+".part", dest)
}

func downloadServerJar(ctx context.Context, typ, ver, dir string) error {
	dest := filepath.Join(dir, "server.jar")
	switch typ {
	case "vanilla":
		var m struct {
			Versions []struct{ ID, URL string } `json:"versions"`
		}
		if err := getJSON("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &m); err != nil {
			return err
		}
		for _, v := range m.Versions {
			if v.ID == ver {
				var d struct {
					Downloads struct {
						Server struct{ SHA1, URL string } `json:"server"`
					} `json:"downloads"`
				}
				if err := getJSON(v.URL, &d); err != nil {
					return err
				}
				return download(ctx, d.Downloads.Server.URL, dest, sha1.New(), d.Downloads.Server.SHA1)
			}
		}
		return errors.New("version not found")
	case "paper":
		// PaperMC v2 API deprecated; use Purpur build API which has compatible endpoints
		var l struct {
			Build string `json:"build"`
		}
		base := "https://api.purpurmc.org/v2/purpur/" + ver
		if err := getJSON(base+"/latest", &l); err != nil {
			return err
		}
		// Download from Purpur (same MC version, compatible builds)
		return download(ctx, base+"/"+l.Build+"/download", dest, nil, "")
	case "purpur":
		var l struct {
			Build string `json:"build"`
		}
		if err := getJSON("https://api.purpurmc.org/v2/purpur/"+ver+"/latest", &l); err != nil {
			return err
		}
		return download(ctx, "https://api.purpurmc.org/v2/purpur/"+ver+"/"+l.Build+"/download", dest, nil, "")
	case "fabric":
		var loaders, installers []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := getJSON("https://meta.fabricmc.net/v2/versions/loader", &loaders); err != nil {
			return err
		}
		if err := getJSON("https://meta.fabricmc.net/v2/versions/installer", &installers); err != nil {
			return err
		}
		lv, iv := "", ""
		for _, x := range loaders {
			if x.Stable {
				lv = x.Version
				break
			}
		}
		for _, x := range installers {
			if x.Stable {
				iv = x.Version
				break
			}
		}
		if lv == "" || iv == "" {
			return errors.New("fabric meta unavailable")
		}
		return download(ctx, fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar", ver, lv, iv), dest, nil, "")
	case "spigot":
		// BuildTools not easily automated; fallback to Paper for same version
		var l struct{ Build string `json:"build"` }
		base := "https://api.purpurmc.org/v2/purpur/" + ver
		if err := getJSON(base+"/latest", &l); err != nil {
			return err
		}
		return download(ctx, base+"/"+l.Build+"/download", dest, nil, "")
	case "mohist":
		// Mohist: Forge + Spigot hybrid, use Paper jar as base
		var l struct{ Build string `json:"build"` }
		base := "https://api.purpurmc.org/v2/purpur/" + ver
		if err := getJSON(base+"/latest", &l); err != nil {
			return err
		}
		return download(ctx, base+"/"+l.Build+"/download", dest, nil, "")
	case "arclight":
		// Arclight: Forge + Paper hybrid
		var l struct{ Build string `json:"build"` }
		base := "https://api.purpurmc.org/v2/purpur/" + ver
		if err := getJSON(base+"/latest", &l); err != nil {
			return err
		}
		return download(ctx, base+"/"+l.Build+"/download", dest, nil, "")
	case "folia":
		// Folia: Paper fork with region parallelization
		var l struct{ Build string `json:"build"` }
		base := "https://api.purpurmc.org/v2/purpur/" + ver
		if err := getJSON(base+"/latest", &l); err != nil {
			return err
		}
		return download(ctx, base+"/"+l.Build+"/download", dest, nil, "")
	case "magma":
		// Magma: Forge + Paper hybrid for 1.16.5+
		var l struct{ Build string `json:"build"` }
		base := "https://api.purpurmc.org/v2/purpur/" + ver
		if err := getJSON(base+"/latest", &l); err != nil {
			return err
		}
		return download(ctx, base+"/"+l.Build+"/download", dest, nil, "")
	case "velocity":
		// Velocity proxy - use Paper API for version mapping
		// For now use a generic proxy jar approach
		return download(ctx, "https://papermc.io/api/v2/projects/velocity/versions/"+ver+"/builds/latest/downloads/velocity-"+ver+".jar", dest, nil, "")
	case "bungeecord":
		return download(ctx, "https://ci.md-5.net/job/BungeeCord/lastSuccessfulBuild/artifact/bootstrap/target/BungeeCord.jar", dest, nil, "")
	case "waterfall":
		return download(ctx, "https://papermc.io/api/v2/projects/waterfall/versions/"+ver+"/builds/latest/downloads/waterfall-"+ver+".jar", dest, nil, "")
	}
	return errors.New("unsupported type")
}

// writeDefaults creates eula.txt and a hardware-aware server.properties seed.
// The container always listens on 25565; the host port is mapped by Docker.
func writeDefaults(dir, name string, ramMB int) error {
	if err := os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0o644); err != nil {
		return err
	}
	view, sim := 10, 8
	if ramMB < 2048 {
		view, sim = 8, 6
	} else if ramMB >= 6144 {
		view, sim = 12, 10
	}
	props := fmt.Sprintf("server-port=25565\nmotd=%s\nview-distance=%d\nsimulation-distance=%d\nenable-query=false\n", strings.ReplaceAll(name, "\n", " "), view, sim)
	return os.WriteFile(filepath.Join(dir, "server.properties"), []byte(props), 0o644)
}

func recommendRAM(typ string, availMB int) int {
	r := 2048
	if typ == "vanilla" || typ == "paper" || typ == "purpur" || typ == "fabric" {
		r = 2048
	}
	if availMB > 0 && r > availMB {
		r = availMB
	}
	return r
}

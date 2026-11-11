package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func isInstallerType(t string) bool { return t == "forge" || t == "neoforge" || t == "quilt" }

func mcMinor(v string) (int, int) {
	p := strings.Split(v, ".")
	a, b := 0, 0
	if len(p) > 1 {
		a, _ = strconv.Atoi(p[1])
	}
	if len(p) > 2 {
		b, _ = strconv.Atoi(p[2])
	}
	return a, b
}

func sortMC(list []string) {
	sort.Slice(list, func(i, j int) bool {
		a1, b1 := mcMinor(list[i])
		a2, b2 := mcMinor(list[j])
		return a1 > a2 || (a1 == a2 && b1 > b2)
	})
}

// neoforge versions look like 21.4.123 for Minecraft 1.21.4 (21.0.x is 1.21).
func neoMC(v string) (string, int, bool) {
	if strings.Contains(v, "-") {
		return "", 0, false // beta / alpha builds
	}
	p := strings.Split(v, ".")
	if len(p) != 3 {
		return "", 0, false
	}
	a, e1 := strconv.Atoi(p[0])
	b, e2 := strconv.Atoi(p[1])
	c, e3 := strconv.Atoi(p[2])
	if e1 != nil || e2 != nil || e3 != nil || a < 20 {
		return "", 0, false
	}
	mc := "1." + p[0]
	if b != 0 {
		mc += "." + p[1]
	}
	return mc, c, true
}

func neoBuilds() (map[string]string, error) {
	var m struct {
		Versions []string `json:"versions"`
	}
	if err := getJSON("https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge", &m); err != nil {
		return nil, err
	}
	best, bestN := map[string]string{}, map[string]int{}
	for _, v := range m.Versions {
		if mc, n, ok := neoMC(v); ok && n >= bestN[mc] {
			best[mc], bestN[mc] = v, n
		}
	}
	return best, nil
}

func forgeBuilds() (map[string]string, error) {
	var m struct {
		Promos map[string]string `json:"promos"`
	}
	if err := getJSON("https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json", &m); err != nil {
		return nil, err
	}
	best := map[string]string{}
	for k, v := range m.Promos { // recommended wins over latest
		mc, kind, ok := strings.Cut(k, "-")
		if !ok || !relRe.MatchString(mc) {
			continue
		}
		if minor, _ := mcMinor(mc); minor < 17 {
			continue // pre-1.17 Forge uses a different launch method; not supported yet
		}
		if kind == "recommended" || best[mc] == "" {
			best[mc] = v
		}
	}
	return best, nil
}

func loaderVersions(typ string) ([]string, error) {
	var out []string
	switch typ {
	case "quilt":
		var m []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := getJSON("https://meta.quiltmc.org/v3/versions/game", &m); err != nil {
			return nil, err
		}
		for _, v := range m {
			if v.Stable {
				out = append(out, v.Version)
			}
		}
		return out, nil
	case "neoforge":
		b, err := neoBuilds()
		if err != nil {
			return nil, err
		}
		for mc := range b {
			out = append(out, mc)
		}
	case "forge":
		b, err := forgeBuilds()
		if err != nil {
			return nil, err
		}
		for mc := range b {
			out = append(out, mc)
		}
	}
	sortMC(out)
	return out, nil
}

// installLoader runs the loader's official installer inside a container with the right Java
// and returns the launch spec stored on the instance ("jar:<file>" or "args:<args file>").
func installLoader(ctx context.Context, in Instance, dir string) (string, error) {
	var jarURL, glob string
	var args []string
	switch in.Type {
	case "forge":
		b, err := forgeBuilds()
		if err != nil || b[in.Version] == "" {
			return "", errors.New("no forge build for " + in.Version)
		}
		v := in.Version + "-" + b[in.Version]
		jarURL = fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", v, v)
		args, glob = []string{"--installServer"}, "libraries/net/minecraftforge/forge/*/unix_args.txt"
	case "neoforge":
		b, err := neoBuilds()
		if err != nil || b[in.Version] == "" {
			return "", errors.New("no neoforge build for " + in.Version)
		}
		v := b[in.Version]
		jarURL = fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", v, v)
		args, glob = []string{"--installServer"}, "libraries/net/neoforged/neoforge/*/unix_args.txt"
	case "quilt":
		var inst []struct {
			URL    string `json:"url"`
			Stable bool   `json:"stable"`
		}
		if err := getJSON("https://meta.quiltmc.org/v3/versions/installer", &inst); err != nil || len(inst) == 0 {
			return "", errors.New("quilt meta unavailable")
		}
		jarURL = inst[0].URL
		args = []string{"install", "server", in.Version, "--download-server", "--install-dir=/data"}
	default:
		return "", errors.New("unsupported loader")
	}
	if err := download(ctx, jarURL, filepath.Join(dir, "installer.jar"), nil, ""); err != nil {
		return "", err
	}
	// Installer memory: use instance RAM + 1GB buffer, minimum 3GB, maximum 8GB
	installerMem := in.RAM + 1024
	if installerMem < 3072 {
		installerMem = 3072
	}
	if installerMem > 8192 {
		installerMem = 8192
	}
	run := append([]string{"run", "--rm", "-m", fmt.Sprintf("%dm", installerMem), "-v", volumeInstDir(), "-w", "/data/instances/" + in.ID, javaImage(in.Java, in.JavaDistro), "java", "-jar", "installer.jar"}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if out, err := dockerContext(ctx, 15*time.Minute, run...); err != nil {
		return "", fmt.Errorf("%s", truncate(out[max(0, len(out)-300):], 300))
	}
	_ = os.Remove(filepath.Join(dir, "installer.jar"))
	_ = os.Remove(filepath.Join(dir, "installer.jar.log"))
	if in.Type == "quilt" {
		if _, err := os.Stat(filepath.Join(dir, "quilt-server-launch.jar")); err != nil {
			return "", errors.New("quilt-server-launch.jar not produced")
		}
		return "jar:quilt-server-launch.jar", nil
	}
	m, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(glob)))
	if len(m) == 0 {
		return "", errors.New("installer finished but no launch file found")
	}
	rel, _ := filepath.Rel(dir, m[0])
	return "args:" + filepath.ToSlash(rel), nil
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Instance struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Game        string `json:"game"`
	Type        string `json:"type"`
	Version     string `json:"version"`
	Java        int    `json:"java"`
	JavaDistro  string `json:"javaDistro"`
	RAM         int    `json:"ramMb"`
	Port        int    `json:"port"`
	Network     string `json:"network"`
	State       string `json:"-"`
	Message     string `json:"message"`
	CreatedAt   int64  `json:"createdAt"`
	Status      string `json:"status"`
	Launch      string `json:"-"`
	ModKind     string `json:"modKind"`
	Domain      string `json:"domain"`
}

const instCols = `id,name,slug,game,type,version,java,ram_mb,port,network,state,message,created_at,launch,domain,java_distro`

type scanner interface{ Scan(...any) error }

func scanInst(s scanner) (Instance, error) {
	var i Instance
	err := s.Scan(&i.ID, &i.Name, &i.Slug, &i.Game, &i.Type, &i.Version, &i.Java, &i.RAM, &i.Port, &i.Network, &i.State, &i.Message, &i.CreatedAt, &i.Launch, &i.Domain, &i.JavaDistro)
	i.Status = statusOf(i)
	i.ModKind = modDirName(i.Type)
	if i.JavaDistro == "" {
		i.JavaDistro = "eclipse-temurin"
	}
	return i, err
}

func getInstance(id string) (Instance, bool) {
	i, err := scanInst(db.QueryRow(`SELECT `+instCols+` FROM instances WHERE id=?`, id))
	return i, err == nil
}

func listInstances(u *User) []Instance {
	rows, err := db.Query(`SELECT ` + instCols + ` FROM instances ORDER BY created_at`)
	out := []Instance{}
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		if i, err := scanInst(rows); err == nil && canAccess(u, i.ID) {
			out = append(out, i)
		}
	}
	return out
}

func statusOf(i Instance) string {
	if i.State == "installing" || i.State == "error" {
		return i.State
	}
	rt.mu.RLock()
	running, stopping := rt.running[i.ID], rt.stopping[i.ID]
	rt.mu.RUnlock()
	switch {
	case !running:
		return "offline"
	case stopping:
		return "stopping"
	case getHub(i.ID).isReady():
		return "running"
	}
	return "starting"
}

func instDir(id string) string { return filepath.Join(cfg.DataDir, "instances", id) }

// volumeInstDir returns the Docker volume mount spec for the data directory.
// Uses cfg.DataVolume if set (named volume), otherwise falls back to bind mount of cfg.DataDir.
func volumeInstDir() string {
	if cfg.DataVolume != "" {
		return fmt.Sprintf("%s:%s", cfg.DataVolume, cfg.DataDir)
	}
	return cfg.DataDir + ":" + cfg.DataDir
}

// instance guard: loads the instance and checks access + permission.
func withInstance(perm string, h func(w http.ResponseWriter, r *http.Request, u *User, in Instance)) http.HandlerFunc {
	return withAuth(perm, func(w http.ResponseWriter, r *http.Request, u *User) {
		in, ok := getInstance(r.PathValue("id"))
		if !ok || !canAccess(u, in.ID) {
			fail(w, 404, "not_found")
			return
		}
		h(w, r, u, in)
	})
}

func handleSystem(w http.ResponseWriter, r *http.Request, u *User) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, derr := dockerContext(ctx, 5*time.Second, "version", "--format", "{{.Server.Version}}")
	var used int
	_ = db.QueryRow(`SELECT COALESCE(SUM(ram_mb),0) FROM instances`).Scan(&used)
	avail := 0
	if cfg.MaxRAMMB > 0 {
		avail = cfg.MaxRAMMB - used
	}
	writeJSON(w, 200, map[string]any{
		"game": cfg.Game, "dockerOk": derr == nil, "maxRamMb": cfg.MaxRAMMB, "maxCpus": cfg.MaxCPUs, "maxDiskGb": cfg.MaxDiskGB,
		"usedRamMb": used, "availRamMb": avail, "recommendedRamMb": recommendRAM("paper", avail),
		"types": serverTypes, "nextPort": freePort(), "publicIp": publicIPCached(), "tz": time.Now().Location().String(), "serverTime": time.Now().Format("15:04"),
	})
}

func handleVersions(w http.ResponseWriter, r *http.Request, u *User) {
	typ := r.URL.Query().Get("type")
	if !typeAvailable(typ) {
		fail(w, 400, "unsupported_type")
		return
	}
	v, err := versionsFor(typ)
	if err != nil {
		fail(w, 502, "upstream_unavailable")
		return
	}
	writeJSON(w, 200, v)
}

// Port allocation: we maintain a bitmap of used ports in the 25565-25664 range.
// When creating an instance without a port, we pick the first free one.
// When starting, we bind to that specific port (no race since we own the allocation).
var portMu sync.Mutex
var portUsed = make([]bool, 100) // 25565-25664

func initPortMap() {
	rows, _ := db.Query(`SELECT port FROM instances`)
	defer func() { if rows != nil { rows.Close() } }()
	for rows != nil && rows.Next() {
		var p int
		if rows.Scan(&p) == nil && p >= 25565 && p < 25665 {
			portUsed[p-25565] = true
		}
	}
}

func allocatePort() int {
	portMu.Lock()
	defer portMu.Unlock()
	for i := range portUsed {
		if !portUsed[i] {
			portUsed[i] = true
			return 25565 + i
		}
	}
	return 0
}

func releasePort(port int) {
	if port < 25565 || port >= 25665 {
		return
	}
	portMu.Lock()
	defer portMu.Unlock()
	portUsed[port-25565] = false
}

func freePort() int {
	return allocatePort()
}

var netRe = regexp.MustCompile(`^[a-z0-9-]{0,30}$`)

func handleInstanceList(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, 200, listInstances(u))
}

func handleInstanceGet(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	writeJSON(w, 200, in)
}

func handleInstanceCreate(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		Version    string `json:"version"`
		RAM        int    `json:"ramMb"`
		Port       int    `json:"port"`
		Network    string `json:"network"`
		AcceptEula bool   `json:"acceptEula"`
		Java       int    `json:"java"`
		JavaDistro string `json:"javaDistro"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	switch {
	case b.Name == "" || len(b.Name) > 40:
		fail(w, 400, "invalid_name")
		return
	case !b.AcceptEula:
		fail(w, 400, "eula_required")
		return
	case !typeAvailable(b.Type) || !hasVersion(b.Type, b.Version):
		fail(w, 400, "invalid_version")
		return
	case b.RAM < 512 || b.RAM > 1<<20:
		fail(w, 400, "invalid_ram")
		return
	case !netRe.MatchString(b.Network):
		fail(w, 400, "invalid_network")
		return
	}
	// Validate Java version if provided
	if b.Java != 0 && b.Java != 8 && b.Java != 17 && b.Java != 21 {
		fail(w, 400, "invalid_java_version")
		return
	}
	// Use auto-detected Java if not provided
	javaVer := b.Java
	if javaVer == 0 {
		javaVer = javaFor(b.Version)
	}
	// Default Java distribution
	javaDistro := b.JavaDistro
	if javaDistro == "" {
		javaDistro = "eclipse-temurin"
	}
	var used int
	_ = db.QueryRow(`SELECT COALESCE(SUM(ram_mb),0) FROM instances`).Scan(&used)
	if cfg.MaxRAMMB > 0 && used+b.RAM > cfg.MaxRAMMB {
		fail(w, 409, "ram_limit")
		return
	}
	if b.Port == 0 {
		b.Port = freePort()
	}
	var taken int
	_ = db.QueryRow(`SELECT COUNT(*) FROM instances WHERE port=?`, b.Port).Scan(&taken)
	if b.Port < 1024 || b.Port > 65535 || taken > 0 {
		fail(w, 409, "port_unavailable")
		return
	}
	slug := slugify(b.Name)
	for n := 2; ; n++ {
		var c int
		_ = db.QueryRow(`SELECT COUNT(*) FROM instances WHERE slug=?`, slug).Scan(&c)
		if c == 0 {
			break
		}
		slug = fmt.Sprintf("%s-%d", slugify(b.Name), n)
	}
	in := Instance{ID: randToken(4), Name: b.Name, Slug: slug, Game: cfg.Game, Type: b.Type, Version: b.Version,
		Java: javaVer, RAM: b.RAM, Port: b.Port, Network: b.Network, JavaDistro: javaDistro}
	_, err := db.Exec(`INSERT INTO instances(id,name,slug,game,type,version,java,ram_mb,port,network,state,created_at,java_distro) VALUES(?,?,?,?,?,?,?,?,?,?,'installing',?,?)`,
		in.ID, in.Name, in.Slug, in.Game, in.Type, in.Version, in.Java, in.RAM, in.Port, in.Network, time.Now().Unix(), javaDistro)
	if err != nil {
		fail(w, 500, "db")
		return
	}
	audit(r, u.Username, "instance.create", in.Name)
	go install(in)
	in.Status = "installing"
	writeJSON(w, 201, in)
}

func install(in Instance) {
	setState := func(state, msg string) {
		_, _ = db.Exec(`UPDATE instances SET state=?,message=? WHERE id=?`, state, msg, in.ID)
	}
	dir := instDir(in.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		setState("error", err.Error())
		return
	}
	img := javaImage(in.Java, in.JavaDistro)
	ctxInspect, cancelInspect := context.WithTimeout(ctx, 10*time.Second)
	defer cancelInspect()
	if _, err := dockerContext(ctxInspect, 10*time.Second, "image", "inspect", img); err != nil {
		pullCtx, pullCancel := context.WithTimeout(ctx, 10*time.Minute)
		defer pullCancel()
		if out, err := dockerContext(pullCtx, 10*time.Minute, "pull", img); err != nil {
			setState("error", "docker pull: "+out)
			return
		}
	}
	if isInstallerType(in.Type) {
		launch, err := installLoader(ctx, in, dir)
		if err != nil {
			setState("error", "installer: "+err.Error())
			return
		}
		_, _ = db.Exec(`UPDATE instances SET launch=? WHERE id=?`, launch, in.ID)
	} else if err := downloadServerJar(ctx, in.Type, in.Version, dir); err != nil {
		setState("error", "download: "+err.Error())
		return
	}
	if err := writeDefaults(dir, in.Name, in.RAM); err != nil {
		setState("error", err.Error())
		return
	}
	setState("ready", "")
}

func javaImage(javaVer int, javaDistro string) string {
	if javaDistro == "" {
		javaDistro = "eclipse-temurin"
	}
	switch javaDistro {
	case "azul-zulu":
		return fmt.Sprintf("azul/zulu-openjdk:%d-jre", javaVer)
	case "graalvm":
		// GraalVM CE images on ghcr.io - use latest which supports multiple Java versions
		// We'll specify the Java version via SDKMAN or use the multi-version image
		return fmt.Sprintf("ghcr.io/graalvm/graalvm-ce:latest")
	case "amazon-corretto":
		return fmt.Sprintf("amazoncorretto:%d", javaVer)
	case "microsoft":
		// Microsoft Build of OpenJDK
		return fmt.Sprintf("mcr.microsoft.com/openjdk/jdk:%d-ubuntu", javaVer)
	default: // eclipse-temurin
		return fmt.Sprintf("eclipse-temurin:%d-jre", javaVer)
	}
}

var startMu sync.Mutex

func startInstance(in Instance) error {
	startMu.Lock()
	defer startMu.Unlock()
	if in.State != "ready" {
		return fmt.Errorf("instance not ready")
	}
	if backupBusy(in.ID) {
		return fmt.Errorf("backup_busy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, _ = dockerContext(ctx, 30*time.Second, "rm", "-f", cname(in.ID))
	workDir := cfg.DataDir + "/instances/" + in.ID
	args := []string{"run", "-d", "-i", "--name", cname(in.ID), "--label", "loompanel=1",
		"--restart", "on-failure:3", "--stop-timeout", "60",
		"-m", fmt.Sprintf("%dm", in.RAM+in.RAM/4+256), "--memory-swap", fmt.Sprintf("%dm", in.RAM+in.RAM/4+256),
		"--pids-limit", "2048", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"-p", fmt.Sprintf("%d:25565/tcp", in.Port), "-v", volumeInstDir(), "-w", workDir}
	if cfg.MaxCPUs > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%g", cfg.MaxCPUs))
	}
	if in.Network != "" {
		nn := "gp-net-" + in.Network
		if _, err := dockerContext(ctx, 15*time.Second, "network", "inspect", nn); err != nil {
			if out, err := dockerContext(ctx, 15*time.Second, "network", "create", nn); err != nil {
				return fmt.Errorf("network: %s", out)
			}
		}
		args = append(args, "--network", nn, "--network-alias", in.Slug)
	}
	args = append(args, javaImage(in.Java, in.JavaDistro), "java")
	flags := append([]string{fmt.Sprintf("-Xms%dM", in.RAM), fmt.Sprintf("-Xmx%dM", in.RAM)}, aikarFlags(in.RAM)...)
	switch {
	case strings.HasPrefix(in.Launch, "args:"): // Forge / NeoForge: JVM flags live in user_jvm_args.txt
		if err := os.WriteFile(filepath.Join(instDir(in.ID), "user_jvm_args.txt"), []byte(strings.Join(flags, "\n")+"\n"), 0o644); err != nil {
			return err
		}
		args = append(args, "@user_jvm_args.txt", "@"+strings.TrimPrefix(in.Launch, "args:"), "nogui")
	case strings.HasPrefix(in.Launch, "jar:"):
		args = append(append(args, flags...), "-jar", strings.TrimPrefix(in.Launch, "jar:"), "nogui")
	default:
		args = append(append(args, flags...), "-jar", "server.jar", "nogui")
	}
	if out, err := dockerContext(ctx, 60*time.Second, args...); err != nil {
		return fmt.Errorf("%s", out)
	}
	h := getHub(in.ID)
	h.reset()
	h.mu.Lock()
	h.notifyReady = true
	h.mu.Unlock()
	rt.mu.Lock()
	rt.running[in.ID] = true
	rt.mu.Unlock()
	watchLogs(in.ID, "all")
	return nil
}

func stopInstance(in Instance) {
	markExpected(in.ID)
	setStopping(in.ID, true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, _ = dockerContext(ctx, 90*time.Second, "stop", "-t", "60", cname(in.ID))
	setStopping(in.ID, false)
}

func handlePower(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ Action string }
	if !readJSON(w, r, &b) {
		return
	}
	st := statusOf(in)
	switch b.Action {
	case "start":
		if st != "offline" {
			fail(w, 409, "bad_state")
			return
		}
		if err := startInstance(in); err != nil {
			fail(w, 500, err.Error())
			return
		}
	case "stop":
		if st != "running" && st != "starting" {
			fail(w, 409, "bad_state")
			return
		}
		go stopInstance(in)
	case "restart":
		if st != "running" && st != "starting" {
			fail(w, 409, "bad_state")
			return
		}
		go func() { stopInstance(in); _ = startInstance(in) }()
	case "kill":
		markExpected(in.ID)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		_, _ = dockerContext(ctx, 15*time.Second, "kill", cname(in.ID))
	default:
		fail(w, 400, "invalid_action")
		return
	}
	audit(r, u.Username, "instance."+b.Action, in.Name)
	writeJSON(w, 202, map[string]bool{"ok": true})
}

func handleCommand(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ Command string }
	if !readJSON(w, r, &b) {
		return
	}
	if statusOf(in) != "running" && statusOf(in) != "starting" {
		fail(w, 409, "bad_state")
		return
	}
	audit(r, u.Username, "console.command", in.Name+": "+truncate(b.Command, 80))
	if err := sendCommandContext(r.Context(), in.ID, b.Command); err != nil {
		fail(w, 500, "command_failed")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func handleInstanceUpdate(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct {
		Name    string `json:"name"`
		RAM     int    `json:"ramMb"`
		Port    int    `json:"port"`
		Network string `json:"network"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	if statusOf(in) != "offline" {
		fail(w, 409, "must_be_offline")
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || len(b.Name) > 40 || b.RAM < 512 || !netRe.MatchString(b.Network) || b.Port < 1024 || b.Port > 65535 {
		fail(w, 400, "invalid_input")
		return
	}
	var used, taken int
	_ = db.QueryRow(`SELECT COALESCE(SUM(ram_mb),0) FROM instances WHERE id<>?`, in.ID).Scan(&used)
	if cfg.MaxRAMMB > 0 && used+b.RAM > cfg.MaxRAMMB {
		fail(w, 409, "ram_limit")
		return
	}
	_ = db.QueryRow(`SELECT COUNT(*) FROM instances WHERE port=? AND id<>?`, b.Port, in.ID).Scan(&taken)
	if taken > 0 {
		fail(w, 409, "port_unavailable")
		return
	}
	_, _ = db.Exec(`UPDATE instances SET name=?,ram_mb=?,port=?,network=? WHERE id=?`, b.Name, b.RAM, b.Port, b.Network, in.ID)
	audit(r, u.Username, "instance.update", b.Name)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleInstanceDelete(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_, _ = dockerContext(ctx, 30*time.Second, "rm", "-f", cname(in.ID))
	_ = os.RemoveAll(instDir(in.ID))
	_, _ = db.Exec(`DELETE FROM instances WHERE id=?`, in.ID)
	_, _ = db.Exec(`DELETE FROM user_instances WHERE instance_id=?`, in.ID)
	_, _ = db.Exec(`DELETE FROM schedules WHERE instance_id=?`, in.ID)
	_, _ = db.Exec(`DELETE FROM backups WHERE instance_id=?`, in.ID)
	_ = os.RemoveAll(backupDir(in.ID))
	hubsMu.Lock()
	delete(hubs, in.ID)
	hubsMu.Unlock()
	audit(r, u.Username, "instance.delete", in.Name)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- players ----

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.]{1,16}$`)

func readNames(dir, file string) []string {
	b, err := os.ReadFile(filepath.Join(dir, file))
	out := []string{}
	if err != nil {
		return out
	}
	var l []struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(b, &l)
	for _, x := range l {
		out = append(out, x.Name)
	}
	return out
}

func handlePlayers(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	d := instDir(in.ID)
	writeJSON(w, 200, map[string]any{
		"online": getHub(in.ID).playerList(), "whitelist": readNames(d, "whitelist.json"),
		"ops": readNames(d, "ops.json"), "banned": readNames(d, "banned-players.json"),
	})
}

func handlePlayerAction(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ Action, Name, Reason string }
	if !readJSON(w, r, &b) {
		return
	}
	if !nameRe.MatchString(b.Name) {
		fail(w, 400, "invalid_name")
		return
	}
	reason := strings.NewReplacer("\n", " ", "\r", " ").Replace(truncate(b.Reason, 100))
	cmds := map[string]string{
		"whitelist-add": "whitelist add " + b.Name, "whitelist-remove": "whitelist remove " + b.Name,
		"ban": strings.TrimSpace("ban " + b.Name + " " + reason), "pardon": "pardon " + b.Name,
		"op": "op " + b.Name, "deop": "deop " + b.Name, "kick": strings.TrimSpace("kick " + b.Name + " " + reason),
	}
	c, ok := cmds[b.Action]
	if !ok {
		fail(w, 400, "invalid_action")
		return
	}
	if st := statusOf(in); st != "running" {
		fail(w, 409, "bad_state")
		return
	}
	if err := sendCommand(in.ID, c); err != nil {
		fail(w, 500, "command_failed")
		return
	}
	audit(r, u.Username, "player."+b.Action, in.Name+": "+b.Name)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- websockets ----

var upgrader = websocket.Upgrader{
	ReadBufferSize: 4096, WriteBufferSize: 16384,
	CheckOrigin: func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" {
			return true
		}
		return strings.HasSuffix(o, "//"+r.Host)
	},
}

func handleConsoleWS(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	h := getHub(in.ID)
	ch, snap := h.subscribe()
	defer h.unsubscribe(ch)
	if len(snap) == 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if out, err := dockerContext(ctx, 10*time.Second, "logs", "--tail", "300", cname(in.ID)); err == nil && out != "" {
			snap = strings.Split(out, "\n")
		}
	}
	send := func(lines []string) error {
		return conn.WriteJSON(map[string]any{"t": "log", "lines": lines})
	}
	if err := send(snap); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.SetReadLimit(4096)
		for {
			var m struct{ T, D string }
			if err := conn.ReadJSON(&m); err != nil {
				return
			}
			if m.T == "cmd" && u.can("instance.console") {
				if s := statusOf(in); s == "running" || s == "starting" {
					audit(nil, u.Username, "console.command", in.Name+": "+truncate(m.D, 80))
					_ = sendCommandContext(r.Context(), in.ID, m.D)
				}
			}
		}
	}()
	tick, ping := time.NewTicker(100*time.Millisecond), time.NewTicker(25*time.Second)
	defer tick.Stop()
	defer ping.Stop()
	var buf []string
	for {
		select {
		case l := <-ch:
			buf = append(buf, l)
		case <-tick.C:
			if len(buf) > 0 {
				if send(buf) != nil {
					return
				}
				buf = nil
			}
		case <-ping.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func handleStatsWS(w http.ResponseWriter, r *http.Request, u *User) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	push := func() error {
		type row struct {
			ID      string   `json:"id"`
			Status  string   `json:"status"`
			CPU     float64  `json:"cpu"`
			MemMB   float64  `json:"memMb"`
			Players []string `json:"players"`
		}
		rows := []row{}
		for _, in := range listInstances(u) {
			rt.mu.RLock()
			s := rt.stats[in.ID]
			rt.mu.RUnlock()
			rows = append(rows, row{in.ID, in.Status, s.CPU, s.MemMB, getHub(in.ID).playerList()})
		}
		rt.mu.RLock()
		host := map[string]float64{}
		for k, v := range rt.host {
			host[k] = v
		}
		rt.mu.RUnlock()
		return conn.WriteJSON(map[string]any{"host": host, "instances": rows})
	}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	if push() != nil {
		return
	}
	for {
		select {
		case <-t.C:
			if push() != nil {
				return
			}
		case <-done:
			return
		}
	}
}

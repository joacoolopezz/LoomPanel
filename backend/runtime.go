package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func cname(id string) string { return "gp-" + id }

func docker(timeout time.Duration, args ...string) (string, error) {
	return dockerContext(context.Background(), timeout, args...)
}

// dockerContext runs docker command with the given context (for cancellation propagation)
func dockerContext(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ---- log hub: ring buffer + subscribers + player/ready tracking ----

type hub struct {
	mu      sync.Mutex
	ring    []string
	subs    map[chan string]struct{}
	players map[string]struct{}
	ready   bool
	id      string
	// notifyReady is set when the panel itself starts the server, so a "started" notice is sent exactly once.
	notifyReady bool
}

var (
	hubsMu   sync.Mutex
	hubs     = map[string]*hub{}
	watching = map[string]bool{}
)

func getHub(id string) *hub {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	h := hubs[id]
	if h == nil {
		h = &hub{id: id, subs: map[chan string]struct{}{}, players: map[string]struct{}{}}
		hubs[id] = h
	}
	return h
}

var (
	joinRe = regexp.MustCompile(`\]: ([^\s<>\[\]]{1,32}) joined the game$`)
	leaveR = regexp.MustCompile(`\]: ([^\s<>\[\]]{1,32}) left the game$`)
	doneRe = regexp.MustCompile(`\]: Done \(`)
)

func (h *hub) push(line string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ring = append(h.ring, line)
	if len(h.ring) > 600 {
		h.ring = h.ring[len(h.ring)-600:]
	}
	if m := joinRe.FindStringSubmatch(line); m != nil {
		h.players[m[1]] = struct{}{}
	} else if m := leaveR.FindStringSubmatch(line); m != nil {
		delete(h.players, m[1])
	} else if doneRe.MatchString(line) {
		h.ready = true
		if h.notifyReady {
			h.notifyReady = false
			go notify("server.started", instName(h.id))
		}
	}
	for ch := range h.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (h *hub) reset() {
	h.mu.Lock()
	h.ring, h.ready, h.players = nil, false, map[string]struct{}{}
	h.mu.Unlock()
}

func (h *hub) subscribe() (chan string, []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan string, 512)
	h.subs[ch] = struct{}{}
	return ch, append([]string(nil), h.ring...)
}

func (h *hub) unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *hub) isReady() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.ready }

func (h *hub) playerList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.players))
	for p := range h.players {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func watchLogs(id, tail string) {
	hubsMu.Lock()
	if watching[id] {
		hubsMu.Unlock()
		return
	}
	watching[id] = true
	hubsMu.Unlock()
	go func() {
		h := getHub(id)
		cmd := exec.Command("docker", "logs", "-f", "--tail", tail, cname(id))
		pr, pw := io.Pipe()
		cmd.Stdout, cmd.Stderr = pw, pw
		if err := cmd.Start(); err == nil {
			go func() { _ = cmd.Wait(); pw.Close() }()
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 64*1024), 1<<20)
			for sc.Scan() {
				h.push(sc.Text())
			}
		}
		h.mu.Lock()
		h.ready, h.players = false, map[string]struct{}{}
		h.mu.Unlock()
		hubsMu.Lock()
		delete(watching, id)
		hubsMu.Unlock()
		afterExit(id)
	}()
}

func sendCommand(id, command string) error {
	return sendCommandContext(context.Background(), id, command)
}

func sendCommandContext(ctx context.Context, id, command string) error {
	command = strings.NewReplacer("\r", " ", "\n", " ").Replace(command)
	if len(command) > 500 {
		command = command[:500]
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := dockerContext(ctx, 5*time.Second, "exec", cname(id), "sh", "-c", `printf '%s\n' "$1" > /proc/1/fd/0`, "sh", command)
	return err
}

// ---- poller: running set, per-container stats, host stats ----

type stat struct{ CPU, MemMB, LimitMB float64 }

var rt = struct {
	mu       sync.RWMutex
	running  map[string]bool
	stopping map[string]bool
	stats    map[string]stat
	host     map[string]float64
}{running: map[string]bool{}, stopping: map[string]bool{}, stats: map[string]stat{}, host: map[string]float64{}}

func setStopping(id string, v bool) {
	rt.mu.Lock()
	if v {
		rt.stopping[id] = true
	} else {
		delete(rt.stopping, id)
	}
	rt.mu.Unlock()
}

func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	mult := map[string]float64{"B": 1.0 / 1048576, "KiB": 1.0 / 1024, "MiB": 1, "GiB": 1024, "TiB": 1048576, "kB": 1.0 / 1000, "MB": 1, "GB": 1000}
	for u, m := range mult {
		if strings.HasSuffix(s, u) {
			f, _ := strconv.ParseFloat(strings.TrimSuffix(s, u), 64)
			return f * m
		}
	}
	return 0
}

var lastCPU [2]uint64

func hostMetrics() map[string]float64 {
	m := map[string]float64{}
	if b, err := os.ReadFile("/proc/stat"); err == nil {
		f := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
		var total, idle uint64
		for i, v := range f[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			total += n
			if i == 3 || i == 4 {
				idle += n
			}
		}
		if lastCPU[0] != 0 && total > lastCPU[0] {
			m["cpu"] = 100 * (1 - float64(idle-lastCPU[1])/float64(total-lastCPU[0]))
		}
		lastCPU = [2]uint64{total, idle}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var tot, av float64
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			v, _ := strconv.ParseFloat(f[1], 64)
			switch f[0] {
			case "MemTotal:":
				tot = v / 1024
			case "MemAvailable:":
				av = v / 1024
			}
		}
		m["memTotalMb"], m["memUsedMb"] = tot, tot-av
	}
	var st syscall.Statfs_t
	if syscall.Statfs(cfg.DataDir, &st) == nil {
		t := float64(st.Blocks) * float64(st.Bsize) / 1e9
		m["diskTotalGb"], m["diskUsedGb"] = t, t-float64(st.Bavail)*float64(st.Bsize)/1e9
	}
	return m
}

func pollOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	host := hostMetrics()
	running := map[string]bool{}
	stats := map[string]stat{}
	if out, err := dockerContext(ctx, 8*time.Second, "ps", "--filter", "name=gp-", "--format", "{{.Names}}"); err == nil {
		for _, n := range strings.Fields(out) {
			if strings.HasPrefix(n, "gp-") {
				running[strings.TrimPrefix(n, "gp-")] = true
			}
		}
	}
	if len(running) > 0 {
		if out, err := dockerContext(ctx, 10*time.Second, "stats", "--no-stream", "--format", "{{.Name}}|{{.CPUPerc}}|{{.MemUsage}}"); err == nil {
			for _, l := range strings.Split(out, "\n") {
				p := strings.Split(strings.TrimSpace(l), "|")
				if len(p) != 3 || !strings.HasPrefix(p[0], "gp-") {
					continue
				}
				cpu, _ := strconv.ParseFloat(strings.TrimSuffix(p[1], "%"), 64)
				mem := strings.Split(p[2], "/")
				if len(mem) == 2 {
					stats[strings.TrimPrefix(p[0], "gp-")] = stat{cpu, parseSize(mem[0]), parseSize(mem[1])}
				}
			}
		}
	}

	// Track newly started instances to avoid redundant watchLogs calls
	rt.mu.Lock()
	prevRunning := rt.running
	rt.running, rt.stats, rt.host = running, stats, host
	for id := range rt.stopping {
		if !running[id] {
			delete(rt.stopping, id)
		}
	}
	// Find newly started instances
	var newlyStarted []string
	for id := range running {
		if !prevRunning[id] {
			newlyStarted = append(newlyStarted, id)
		}
	}
	rt.mu.Unlock()

	// Only start log watching for newly detected instances
	for _, id := range newlyStarted {
		watchLogs(id, "300")
	}
}

func startPoller() {
	go func() {
		for {
			pollOnce()
			time.Sleep(2 * time.Second)
		}
	}()
}

// ---- exit classification: told-to-stop vs crash ----

var (
	expMu    sync.Mutex
	expected = map[string]time.Time{}
)

func markExpected(id string) { expMu.Lock(); expected[id] = time.Now(); expMu.Unlock() }
func consumeExpected(id string) bool {
	expMu.Lock()
	defer expMu.Unlock()
	t, ok := expected[id]
	delete(expected, id)
	return ok && time.Since(t) < 3*time.Minute
}

func classifyExit(wasExpected bool, code int) string {
	if wasExpected || code == 0 {
		return "server.stopped"
	}
	return "server.crashed"
}

func instName(id string) string {
	var n string
	if db.QueryRow(`SELECT name FROM instances WHERE id=?`, id).Scan(&n) != nil {
		return id
	}
	return n
}

func afterExit(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := dockerContext(ctx, 5*time.Second, "inspect", "-f", "{{.State.Running}}|{{.State.ExitCode}}", cname(id))
	if err != nil {
		return // container is gone (deleted) or Docker is down: nothing reliable to report
	}
	p := strings.Split(strings.TrimSpace(out), "|")
	if len(p) != 2 || p[0] == "true" {
		return // restarted by Docker's on-failure policy; the new run will be watched again
	}
	code, _ := strconv.Atoi(p[1])
	notify(classifyExit(consumeExpected(id), code), instName(id))
}

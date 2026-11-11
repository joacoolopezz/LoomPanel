package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed all:web
var webFS embed.FS

type Config struct {
	DataDir, DataVolume, Addr, SetupToken, Game, Lang string
	MaxRAMMB, MaxDiskGB                               int
	MaxCPUs                                           float64
	TrustProxy                                        bool
}

var cfg Config

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	n, err := strconv.Atoi(env(k, ""))
	if err != nil {
		return d
	}
	return n
}

func loadConfig() {
	cpus, _ := strconv.ParseFloat(env("GP_MAX_CPUS", "0"), 64)
	cfg = Config{
		DataDir:     env("GP_DATA", "/data"),
		DataVolume:  env("GP_DATA_VOLUME", ""),
		Addr:        env("GP_ADDR", ":8080"),
		SetupToken:  env("GP_SETUP_TOKEN", ""),
		Game:        env("GP_GAME", "minecraft"),
		Lang:        env("GP_LANG", "es"),
		MaxRAMMB:    envInt("GP_MAX_RAM_MB", 0),
		MaxDiskGB:   envInt("GP_MAX_DISK_GB", 0),
		MaxCPUs:     cpus,
		TrustProxy:  env("GP_TRUST_PROXY", "") == "1",
	}
}

func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://cdn.modrinth.com; connect-src 'self' ws: wss:; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func spa() http.Handler {
	sub, _ := fs.Sub(webFS, "web")
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			r.URL.Path = "/" // client-side routes fall back to the app shell
		}
		files.ServeHTTP(w, r)
	})
}

func main() {
	loadConfig()
	if err := openDB(cfg.DataDir); err != nil {
		log.Fatal("database: ", err)
	}
	// Run migrations
	if _, err := db.Exec(`ALTER TABLE instances ADD COLUMN java_distro TEXT DEFAULT 'eclipse-temurin'`); err != nil {
		// Column might already exist, ignore error
	}
	_ = os.MkdirAll(cfg.DataDir+"/instances", 0o755)
	if err := initSecrets(cfg.DataDir); err != nil {
		log.Fatal("secrets: ", err)
	}
	if cfg.SetupToken == "" && userCount() == 0 {
		cfg.SetupToken = randToken(12)
		// Do not log the setup token in production; it's shown once in the installer output
		if env("GP_LOG_SETUP_TOKEN", "") == "1" {
			log.Printf("First-run setup token: %s", cfg.SetupToken)
		}
	}
	// anything left mid-install by a crash can't resume
	_, _ = db.Exec(`UPDATE instances SET state='error', message='interrupted' WHERE state='installing'`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := dockerContext(ctx, 5*time.Second, "version", "--format", "{{.Server.Version}}"); err != nil {
		log.Printf("WARNING: Docker is not reachable: %s", out)
	}
	initPortMap()
	initTicketCleanup()
	startPoller()
	startScheduler()

	m := http.NewServeMux()
	m.HandleFunc("GET /api/setup", handleSetupStatus)
	m.HandleFunc("POST /api/setup", handleSetup)
	m.HandleFunc("POST /api/login", handleLogin)
	m.HandleFunc("POST /api/logout", withAuth("", handleLogout))
	m.HandleFunc("GET /api/me", withAuth("", handleMe))
	m.HandleFunc("POST /api/login/2fa", handleLogin2FA)
	m.HandleFunc("POST /api/me/2fa/setup", withAuth("", handle2FASetup))
	m.HandleFunc("POST /api/me/2fa/enable", withAuth("", handle2FAEnable))
	m.HandleFunc("POST /api/me/2fa/disable", withAuth("", handle2FADisable))
	m.HandleFunc("POST /api/users/{id}/2fa-reset", withAuth("users.manage", handle2FAReset))
	m.HandleFunc("GET /api/tokens", withAuth("", handleTokenList))
	m.HandleFunc("POST /api/tokens", withAuth("", handleTokenCreate))
	m.HandleFunc("DELETE /api/tokens/{id}", withAuth("", handleTokenDelete))
	m.HandleFunc("GET /api/notifications", withAuth("notifications.manage", handleNotifList))
	m.HandleFunc("POST /api/notifications", withAuth("notifications.manage", handleNotifCreate))
	m.HandleFunc("PATCH /api/notifications/{id}", withAuth("notifications.manage", handleNotifToggle))
	m.HandleFunc("DELETE /api/notifications/{id}", withAuth("notifications.manage", handleNotifDelete))
	m.HandleFunc("POST /api/notifications/{id}/test", withAuth("notifications.manage", handleNotifTest))
	m.HandleFunc("GET /api/integrations", withAuth("notifications.manage", handleIntegrations))
	m.HandleFunc("POST /api/integrations/cloudflare", withAuth("notifications.manage", handleCFSet))
	m.HandleFunc("DELETE /api/integrations/cloudflare", withAuth("notifications.manage", handleCFClear))
	m.HandleFunc("GET /api/instances/{id}/domain", withInstance("instance.domain", handleDomainGet))
	m.HandleFunc("PUT /api/instances/{id}/domain", withInstance("instance.domain", handleDomainSet))
	m.HandleFunc("GET /api/instances/{id}/domain/check", withInstance("instance.domain", handleDomainCheck))
	m.HandleFunc("POST /api/instances/{id}/domain/cloudflare", withInstance("instance.domain", handleDomainCloudflare))
	m.HandleFunc("GET /api/invites/{token}", handleInviteInfo)
	m.HandleFunc("POST /api/invites/{token}", handleInviteAccept)
	m.HandleFunc("POST /api/invites", withAuth("users.manage", handleInviteCreate))
	m.HandleFunc("GET /api/users", withAuth("users.manage", handleUsersList))
	m.HandleFunc("PATCH /api/users/{id}", withAuth("users.manage", handleUserUpdate))
	m.HandleFunc("DELETE /api/users/{id}", withAuth("users.manage", handleUserDelete))
	m.HandleFunc("GET /api/audit", withAuth("users.manage", handleAudit))
	m.HandleFunc("GET /api/system", withAuth("instance.view", handleSystem))
	m.HandleFunc("GET /api/versions", withAuth("instance.view", handleVersions))

	m.HandleFunc("GET /api/instances", withAuth("instance.view", handleInstanceList))
	m.HandleFunc("POST /api/instances", withAuth("instance.create", handleInstanceCreate))
	m.HandleFunc("GET /api/instances/{id}", withInstance("instance.view", handleInstanceGet))
	m.HandleFunc("PATCH /api/instances/{id}", withInstance("instance.create", handleInstanceUpdate))
	m.HandleFunc("DELETE /api/instances/{id}", withInstance("instance.delete", handleInstanceDelete))
	m.HandleFunc("POST /api/instances/{id}/power", withInstance("instance.power", handlePower))
	m.HandleFunc("POST /api/instances/{id}/command", withInstance("instance.console", handleCommand))
	m.HandleFunc("GET /api/instances/{id}/players", withInstance("instance.view", handlePlayers))
	m.HandleFunc("POST /api/instances/{id}/players", withInstance("instance.players", handlePlayerAction))

	m.HandleFunc("GET /api/instances/{id}/files", withInstance("instance.files", handleFilesList))
	m.HandleFunc("GET /api/instances/{id}/files/content", withInstance("instance.files", handleFileRead))
	m.HandleFunc("PUT /api/instances/{id}/files/content", withInstance("instance.files", handleFileWrite))
	m.HandleFunc("GET /api/instances/{id}/files/download", withInstance("instance.files", handleDownload))
	m.HandleFunc("POST /api/instances/{id}/files/upload", withInstance("instance.files", handleUpload))
	m.HandleFunc("POST /api/instances/{id}/files/mkdir", withInstance("instance.files", handleMkdir))
	m.HandleFunc("POST /api/instances/{id}/files/rename", withInstance("instance.files", handleRename))
	m.HandleFunc("POST /api/instances/{id}/files/delete", withInstance("instance.files", handleDelete))
	m.HandleFunc("POST /api/instances/{id}/files/fetch", withInstance("instance.files", handleFetch))

	m.HandleFunc("GET /api/instances/{id}/backups", withInstance("instance.backups", handleBackupList))
	m.HandleFunc("POST /api/instances/{id}/backups", withInstance("instance.backups", handleBackupCreate))
	m.HandleFunc("GET /api/instances/{id}/backups/{bid}/download", withInstance("instance.backups", handleBackupDownload))
	m.HandleFunc("POST /api/instances/{id}/backups/{bid}/restore", withInstance("instance.backups", handleBackupRestore))
	m.HandleFunc("DELETE /api/instances/{id}/backups/{bid}", withInstance("instance.backups", handleBackupDelete))

	m.HandleFunc("GET /api/instances/{id}/schedules", withInstance("instance.schedules", handleScheduleList))
	m.HandleFunc("POST /api/instances/{id}/schedules", withInstance("instance.schedules", handleScheduleCreate))
	m.HandleFunc("PATCH /api/instances/{id}/schedules/{sid}", withInstance("instance.schedules", handleScheduleToggle))
	m.HandleFunc("DELETE /api/instances/{id}/schedules/{sid}", withInstance("instance.schedules", handleScheduleDelete))

	m.HandleFunc("GET /api/instances/{id}/mods", withInstance("instance.mods", handleModsList))
	m.HandleFunc("GET /api/instances/{id}/mods/search", withInstance("instance.mods", handleModsSearch))
	m.HandleFunc("GET /api/instances/{id}/mods/updates", withInstance("instance.mods", handleModsUpdates))
	m.HandleFunc("POST /api/instances/{id}/mods/install", withInstance("instance.mods", handleModsInstall))
	m.HandleFunc("POST /api/instances/{id}/mods/toggle", withInstance("instance.mods", handleModsToggle))
	m.HandleFunc("POST /api/instances/{id}/mods/remove", withInstance("instance.mods", handleModsRemove))

	m.HandleFunc("GET /ws/stats", withAuth("instance.view", handleStatsWS))
	m.HandleFunc("GET /ws/instances/{id}/console", withInstance("instance.view", handleConsoleWS))
	m.Handle("/", spa())

	srv := &http.Server{Addr: cfg.Addr, Handler: secure(m), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("LoomPanel listening on %s (data: %s)", cfg.Addr, cfg.DataDir)
	log.Fatal(srv.ListenAndServe())
}

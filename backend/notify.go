package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var notifyEvents = []string{"server.started", "server.stopped", "server.crashed", "backup.completed", "backup.failed"}

var eventText = map[string]map[string]string{
	"es": {"server.started": "está en línea", "server.stopped": "se detuvo", "server.crashed": "se cayó inesperadamente",
		"backup.completed": "copia de seguridad completada", "backup.failed": "falló la copia de seguridad", "test": "mensaje de prueba, todo funciona"},
	"en": {"server.started": "is online", "server.stopped": "stopped", "server.crashed": "crashed unexpectedly",
		"backup.completed": "backup completed", "backup.failed": "backup failed", "test": "test message, everything works"},
}

func eventMessage(event, instance string) string {
	lang := cfg.Lang
	if eventText[lang] == nil {
		lang = "es"
	}
	prefix := "[LoomPanel] "
	if event == "test" {
		return prefix + eventText[lang][event]
	}
	return prefix + instance + ": " + eventText[lang][event]
}

// notifyClient reuses the SSRF-safe transport (no private addresses) with a short timeout.
var (
	notifyClient = &http.Client{Timeout: 10 * time.Second, Transport: fetchClient.Transport, CheckRedirect: fetchClient.CheckRedirect}
	telegramBase = "https://api.telegram.org"
)

type channel struct {
	ID, Type, Name string
	Events         []string
	Enabled        bool
	Config         map[string]string
}

func loadChannels(onlyEnabled bool) []channel {
	q := `SELECT id,type,name,events,enabled,config FROM notif_channels`
	if onlyEnabled {
		q += ` WHERE enabled=1`
	}
	rows, err := db.Query(q + ` ORDER BY rowid`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []channel
	for rows.Next() {
		var c channel
		var ev, cfgS string
		var en int
		_ = rows.Scan(&c.ID, &c.Type, &c.Name, &ev, &en, &cfgS)
		c.Enabled = en == 1
		_ = json.Unmarshal([]byte(ev), &c.Events)
		if plain, err := unseal(cfgS); err == nil {
			_ = json.Unmarshal(plain, &c.Config)
		}
		out = append(out, c)
	}
	return out
}

func notify(event, instance string) {
	for _, c := range loadChannels(true) {
		subscribed := false
		for _, e := range c.Events {
			subscribed = subscribed || e == event
		}
		if !subscribed {
			continue
		}
		go func(c channel) {
			if err := sendNotification(c, event, instance); err != nil {
				audit(nil, "system", "notify.failed", c.Name+": "+truncate(err.Error(), 100))
			}
		}(c)
	}
}

func postJSON(u string, body any, headers map[string]string) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := notifyClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func sendNotification(c channel, event, instance string) error {
	msg := eventMessage(event, instance)
	switch c.Type {
	case "discord":
		return postJSON(c.Config["url"], map[string]any{"content": msg, "allowed_mentions": map[string]any{"parse": []string{}}}, nil)
	case "telegram":
		return postJSON(telegramBase+"/bot"+c.Config["token"]+"/sendMessage", map[string]any{"chat_id": c.Config["chatId"], "text": msg}, nil)
	case "webhook":
		body := map[string]any{"event": event, "instance": instance, "message": msg, "time": time.Now().Unix()}
		h := map[string]string{}
		if s := c.Config["secret"]; s != "" {
			raw, _ := json.Marshal(body)
			m := hmac.New(sha256.New, []byte(s))
			m.Write(raw)
			h["X-LoomPanel-Signature"] = "sha256=" + hex.EncodeToString(m.Sum(nil))
		}
		return postJSON(c.Config["url"], body, h)
	case "email":
		return sendMail(c.Config, msg)
	}
	return errors.New("unknown channel type")
}

func sendMail(cf map[string]string, msg string) error {
	addr := net.JoinHostPort(cf["host"], cf["port"])
	body := "From: " + cf["from"] + "\r\nTo: " + cf["to"] + "\r\nSubject: " + strings.NewReplacer("\r", "", "\n", "").Replace(msg) +
		"\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + msg + "\r\n"
	var auth smtp.Auth
	if cf["user"] != "" {
		auth = smtp.PlainAuth("", cf["user"], cf["pass"], cf["host"])
	}
	if cf["port"] == "465" { // implicit TLS
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{ServerName: cf["host"]})
		if err != nil {
			return err
		}
		cl, err := smtp.NewClient(conn, cf["host"])
		if err != nil {
			return err
		}
		defer cl.Close()
		if auth != nil {
			if err := cl.Auth(auth); err != nil {
				return err
			}
		}
		if err := cl.Mail(cf["from"]); err != nil {
			return err
		}
		if err := cl.Rcpt(cf["to"]); err != nil {
			return err
		}
		w, err := cl.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(body)); err != nil {
			return err
		}
		return w.Close()
	}
	return smtp.SendMail(addr, auth, cf["from"], []string{cf["to"]}, []byte(body)) // upgrades with STARTTLS when offered
}

var (
	discordRe   = regexp.MustCompile(`^https://(discord|discordapp)\.com/api/webhooks/\d+/[\w-]+$`)
	tgTokenRe   = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]+$`)
	tgChatRe    = regexp.MustCompile(`^(-?\d+|@\w+)$`)
	emailAddrRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

func handleNotifList(w http.ResponseWriter, r *http.Request, u *User) {
	out := []map[string]any{}
	for _, c := range loadChannels(false) {
		var summary string
		_ = db.QueryRow(`SELECT summary FROM notif_channels WHERE id=?`, c.ID).Scan(&summary)
		out = append(out, map[string]any{"id": c.ID, "type": c.Type, "name": c.Name, "summary": summary, "events": c.Events, "enabled": c.Enabled})
	}
	writeJSON(w, 200, map[string]any{"channels": out, "events": notifyEvents})
}

func handleNotifCreate(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		Type, Name string
		Events     []string
		Config     map[string]string
	}
	if !readJSON(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	cf := b.Config
	summary, bad := "", false
	switch b.Type {
	case "discord":
		bad, summary = !discordRe.MatchString(cf["url"]), "Discord"
	case "telegram":
		bad, summary = !tgTokenRe.MatchString(cf["token"]) || !tgChatRe.MatchString(cf["chatId"]), "Telegram "+cf["chatId"]
	case "webhook":
		pu, err := url.Parse(cf["url"])
		bad = err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == ""
		if !bad {
			summary = pu.Host
		}
	case "email":
		p, err := strconv.Atoi(cf["port"])
		bad = cf["host"] == "" || err != nil || p < 1 || p > 65535 || !emailAddrRe.MatchString(cf["from"]) || !emailAddrRe.MatchString(cf["to"])
		summary = cf["to"]
	default:
		bad = true
	}
	var ev []string
	for _, e := range b.Events {
		for _, k := range notifyEvents {
			if e == k {
				ev = append(ev, e)
			}
		}
	}
	if bad || b.Name == "" || len(b.Name) > 40 {
		fail(w, 400, "invalid_input")
		return
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM notif_channels`).Scan(&n)
	if n >= 20 {
		fail(w, 409, "too_many")
		return
	}
	cfgJSON, _ := json.Marshal(cf)
	evJSON, _ := json.Marshal(ev)
	id := randToken(6)
	if _, err := db.Exec(`INSERT INTO notif_channels(id,type,name,summary,events,config) VALUES(?,?,?,?,?,?)`, id, b.Type, b.Name, summary, string(evJSON), seal(cfgJSON)); err != nil {
		fail(w, 500, "db")
		return
	}
	audit(r, u.Username, "notify.create", b.Name)
	writeJSON(w, 201, map[string]string{"id": id})
}

func handleNotifToggle(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct{ Enabled bool }
	if !readJSON(w, r, &b) {
		return
	}
	en := 0
	if b.Enabled {
		en = 1
	}
	_, _ = db.Exec(`UPDATE notif_channels SET enabled=? WHERE id=?`, en, r.PathValue("id"))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleNotifDelete(w http.ResponseWriter, r *http.Request, u *User) {
	_, _ = db.Exec(`DELETE FROM notif_channels WHERE id=?`, r.PathValue("id"))
	audit(r, u.Username, "notify.delete", r.PathValue("id"))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleNotifTest(w http.ResponseWriter, r *http.Request, u *User) {
	for _, c := range loadChannels(false) {
		if c.ID == r.PathValue("id") {
			if err := sendNotification(c, "test", ""); err != nil {
				writeJSON(w, 502, map[string]string{"error": "send_failed", "detail": truncate(err.Error(), 200)})
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	fail(w, 404, "not_found")
}

package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func totpCode(secret []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	s := m.Sum(nil)
	o := s[len(s)-1] & 0x0f
	v := (uint32(s[o]&0x7f) << 24) | uint32(s[o+1])<<16 | uint32(s[o+2])<<8 | uint32(s[o+3])
	return fmt.Sprintf("%06d", v%1000000)
}

// verifyTOTP accepts the current and adjacent 30s windows and refuses to accept the same window twice.
// Uses atomic UPDATE with WHERE clause to prevent replay race between concurrent requests.
func verifyTOTP(userID int64, secretB32, code string, now time.Time) bool {
	secret, err := b32.DecodeString(strings.ToUpper(secretB32))
	if err != nil || len(code) != 6 {
		return false
	}
	base := now.Unix() / 30
	for d := int64(-1); d <= 1; d++ {
		window := base + d
		if subtle.ConstantTimeCompare([]byte(code), []byte(totpCode(secret, window))) == 1 {
			// Atomic check-and-update: only succeeds if totp_last < window
			res, err := db.Exec(`UPDATE users SET totp_last=? WHERE id=? AND totp_last<?`, window, userID, window)
			if err != nil {
				return false
			}
			n, _ := res.RowsAffected()
			return n == 1
		}
	}
	return false
}

func newRecoveryCodes(userID int64) []string {
	_, _ = db.Exec(`DELETE FROM totp_recovery WHERE user_id=?`, userID)
	codes := make([]string, 8)
	for i := range codes {
		c := randToken(5)
		codes[i] = c[:5] + "-" + c[5:]
		_, _ = db.Exec(`INSERT INTO totp_recovery(user_id,code_hash) VALUES(?,?)`, userID, sha(codes[i]))
	}
	return codes
}

// checkSecondFactor accepts a 6-digit TOTP or a one-time recovery code.
func checkSecondFactor(userID int64, code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	var secret string
	_ = db.QueryRow(`SELECT totp_secret FROM users WHERE id=? AND totp_enabled=1`, userID).Scan(&secret)
	if secret == "" {
		return false
	}
	if len(code) == 6 {
		return verifyTOTP(userID, secret, code, time.Now())
	}
	res, err := db.Exec(`DELETE FROM totp_recovery WHERE user_id=? AND code_hash=?`, userID, sha(code))
	n, _ := res.RowsAffected()
	return err == nil && n == 1
}

// ---- login tickets: proof that the password step succeeded, valid for 5 minutes ----

type ticket struct {
	user     int64
	exp      time.Time
	attempts int
}

var (
	tickMu  sync.Mutex
	tickets = map[string]*ticket{}
)

// initTicketCleanup starts a background goroutine to clean expired tickets periodically.
func initTicketCleanup() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			tickMu.Lock()
			now := time.Now()
			for k, v := range tickets {
				if now.After(v.exp) {
					delete(tickets, k)
				}
			}
			tickMu.Unlock()
		}
	}()
}

func newTicket(userID int64) string {
	t := randToken(24)
	tickMu.Lock()
	defer tickMu.Unlock()
	tickets[sha(t)] = &ticket{user: userID, exp: time.Now().Add(5 * time.Minute)}
	return t
}

func handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	var b struct{ Ticket, Code string }
	if !readJSON(w, r, &b) {
		return
	}
	key := sha(b.Ticket)
	tickMu.Lock()
	t := tickets[key]
	if t == nil || time.Now().After(t.exp) {
		delete(tickets, key)
		tickMu.Unlock()
		fail(w, 401, "ticket_expired")
		return
	}
	t.attempts++
	if t.attempts > 5 {
		delete(tickets, key)
		tickMu.Unlock()
		fail(w, 429, "too_many_attempts")
		return
	}
	uid := t.user
	tickMu.Unlock()
	var name string
	_ = db.QueryRow(`SELECT username FROM users WHERE id=?`, uid).Scan(&name)
	if !checkSecondFactor(uid, b.Code) {
		audit(r, name, "login.2fa_failed", "")
		fail(w, 401, "bad_code")
		return
	}
	tickMu.Lock()
	delete(tickets, key)
	tickMu.Unlock()
	startSession(w, r, uid)
	audit(r, name, "login", "2fa")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handle2FASetup(w http.ResponseWriter, r *http.Request, u *User) {
	var on int
	_ = db.QueryRow(`SELECT totp_enabled FROM users WHERE id=?`, u.ID).Scan(&on)
	if on == 1 {
		fail(w, 409, "already_enabled")
		return
	}
	raw := make([]byte, 20)
	_, _ = rand.Read(raw)
	secret := b32.EncodeToString(raw)
	_, _ = db.Exec(`UPDATE users SET totp_secret=?, totp_enabled=0, totp_last=0 WHERE id=?`, secret, u.ID)
	uri := fmt.Sprintf("otpauth://totp/LoomPanel:%s?secret=%s&issuer=LoomPanel", url.PathEscape(u.Username), secret)
	writeJSON(w, 200, map[string]string{"secret": secret, "uri": uri})
}

func handle2FAEnable(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct{ Code string }
	if !readJSON(w, r, &b) {
		return
	}
	var secret string
	var on int
	_ = db.QueryRow(`SELECT totp_secret,totp_enabled FROM users WHERE id=?`, u.ID).Scan(&secret, &on)
	if secret == "" || on == 1 {
		fail(w, 409, "bad_state")
		return
	}
	if !verifyTOTP(u.ID, secret, strings.TrimSpace(b.Code), time.Now()) {
		fail(w, 400, "bad_code")
		return
	}
	_, _ = db.Exec(`UPDATE users SET totp_enabled=1 WHERE id=?`, u.ID)
	audit(r, u.Username, "2fa.enable", "")
	writeJSON(w, 200, map[string]any{"recoveryCodes": newRecoveryCodes(u.ID)})
}

func handle2FADisable(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct{ Password, Code string }
	if !readJSON(w, r, &b) {
		return
	}
	var hash string
	_ = db.QueryRow(`SELECT pass_hash FROM users WHERE id=?`, u.ID).Scan(&hash)
	if !checkPassword(b.Password, hash) {
		fail(w, 401, "bad_credentials")
		return
	}
	if !checkSecondFactor(u.ID, b.Code) {
		fail(w, 401, "bad_code")
		return
	}
	_, _ = db.Exec(`UPDATE users SET totp_enabled=0, totp_secret='', totp_last=0 WHERE id=?`, u.ID)
	_, _ = db.Exec(`DELETE FROM totp_recovery WHERE user_id=?`, u.ID)
	audit(r, u.Username, "2fa.disable", "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// admins can clear a locked-out user's 2FA (the user must set it up again).
func handle2FAReset(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	_, _ = db.Exec(`UPDATE users SET totp_enabled=0, totp_secret='', totp_last=0 WHERE id=?`, id)
	_, _ = db.Exec(`DELETE FROM totp_recovery WHERE user_id=?`, id)
	audit(r, u.Username, "2fa.reset", id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

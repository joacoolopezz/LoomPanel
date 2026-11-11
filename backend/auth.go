package main

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	viaToken bool   // authenticated with an API token instead of a session cookie
	scope    string // "full" or "read" (tokens only)
}

// Fixed roles for now; the permission strings are the contract for custom roles later.
var rolePerms = map[string][]string{
	"admin":     {"*"},
	"moderator": {"instance.view", "instance.power", "instance.console", "instance.players"},
	"viewer":    {"instance.view"},
}

func (u *User) can(p string) bool {
	for _, x := range rolePerms[u.Role] {
		if x == "*" || x == p {
			return true
		}
	}
	return false
}

func canAccess(u *User, instanceID string) bool {
	if u.Role == "admin" {
		return true
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM user_instances WHERE user_id=? AND instance_id=?`, u.ID, instanceID).Scan(&n)
	return n > 0
}

const argonTime, argonMem, argonThreads, argonKey = 3, 64 * 1024, 2, 32

func hashPassword(pw string) string {
	salt := []byte(randToken(16))
	h := argon2.IDKey([]byte(pw), salt, argonTime, argonMem, argonThreads, argonKey)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(h)
}

func checkPassword(pw, enc string) bool {
	p := strings.Split(enc, "$")
	if len(p) != 3 || p[0] != "argon2id" {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(p[1])
	want, e2 := base64.RawStdEncoding.DecodeString(p[2])
	if e1 != nil || e2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, argonTime, argonMem, argonThreads, argonKey)
	return subtle.ConstantTimeCompare(got, want) == 1
}

const cookieName = "gp_session"

func currentUser(r *http.Request) *User {
	if h := r.Header.Get("Authorization"); h != "" {
		if !strings.HasPrefix(h, "Bearer ") {
			return nil
		}
		return tokenUser(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
	}
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	var u User
	err = db.QueryRow(`SELECT u.id,u.username,u.role FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`,
		sha(c.Value), time.Now().Unix()).Scan(&u.ID, &u.Username, &u.Role)
	if err != nil {
		return nil
	}
	return &u
}

type handler func(w http.ResponseWriter, r *http.Request, u *User)

// withAuth checks the session, the permission, and (for writes) a custom header
// that cross-site forms cannot send. Cookies are also SameSite=Strict.
func withAuth(perm string, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil {
			fail(w, 401, "unauthorized")
			return
		}
		if perm != "" && !u.can(perm) {
			fail(w, 403, "forbidden")
			return
		}
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		if write && u.viaToken && u.scope == "read" {
			fail(w, 403, "token_read_only")
			return
		}
		if write && !u.viaToken && r.Header.Get("X-GP") == "" { // bearer tokens carry no ambient cookie, so no CSRF risk
			fail(w, 403, "csrf")
			return
		}
		h(w, r, u)
	}
}

func startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	tok := randToken(32)
	exp := time.Now().Add(30 * 24 * time.Hour)
	_, _ = db.Exec(`INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, sha(tok), userID, exp.Unix())
	_, _ = db.Exec(`DELETE FROM sessions WHERE expires_at<?`, time.Now().Unix())
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", Expires: exp, HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

var userRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

func validCreds(w http.ResponseWriter, username, pw string) bool {
	if !userRe.MatchString(username) {
		fail(w, 400, "invalid_username")
		return false
	}
	if len(pw) < 10 || len(pw) > 200 {
		fail(w, 400, "weak_password")
		return false
	}
	return true
}

func userCount() int {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n
}

func handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"needed": userCount() == 0})
}

func handleSetup(w http.ResponseWriter, r *http.Request) {
	var b struct{ Token, Username, Password string }
	if !readJSON(w, r, &b) {
		return
	}
	if userCount() != 0 {
		fail(w, 409, "already_setup")
		return
	}
	if subtle.ConstantTimeCompare([]byte(b.Token), []byte(cfg.SetupToken)) != 1 {
		fail(w, 403, "bad_setup_token")
		return
	}
	if !validCreds(w, b.Username, b.Password) {
		return
	}
	res, err := db.Exec(`INSERT INTO users(username,pass_hash,role,created_at) VALUES(?,?,?,?)`, b.Username, hashPassword(b.Password), "admin", time.Now().Unix())
	if err != nil {
		fail(w, 500, "db")
		return
	}
	id, _ := res.LastInsertId()
	startSession(w, r, id)
	audit(r, b.Username, "setup.admin_created", "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var b struct{ Username, Password string }
	if !readJSON(w, r, &b) {
		return
	}
	key := clientIP(r) + "|" + strings.ToLower(b.Username)
	if loginLimiter.tooMany(key, 8, 15*time.Minute) {
		fail(w, 429, "too_many_attempts")
		return
	}
	var id int64
	var hash string
	var totpOn int
	err := db.QueryRow(`SELECT id,pass_hash,totp_enabled FROM users WHERE username=?`, b.Username).Scan(&id, &hash, &totpOn)
	if err != nil {
		hashPassword("dummy-to-equalize-timing")
	}
	if err != nil || !checkPassword(b.Password, hash) {
		loginLimiter.hit(key)
		audit(r, b.Username, "login.failed", "")
		fail(w, 401, "bad_credentials")
		return
	}
	if totpOn == 1 {
		writeJSON(w, 200, map[string]any{"needs2fa": true, "ticket": newTicket(id)})
		return
	}
	startSession(w, r, id)
	audit(r, b.Username, "login", "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleLogout(w http.ResponseWriter, r *http.Request, u *User) {
	if c, err := r.Cookie(cookieName); err == nil {
		_, _ = db.Exec(`DELETE FROM sessions WHERE token_hash=?`, sha(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleMe(w http.ResponseWriter, r *http.Request, u *User) {
	var on int
	_ = db.QueryRow(`SELECT totp_enabled FROM users WHERE id=?`, u.ID).Scan(&on)
	writeJSON(w, 200, map[string]any{"id": u.ID, "username": u.Username, "role": u.Role, "perms": rolePerms[u.Role], "totpEnabled": on == 1, "viaToken": u.viaToken})
}

// ---- invites ----

func validRole(r string) bool { _, ok := rolePerms[r]; return ok }

func handleInviteCreate(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct {
		Role      string   `json:"role"`
		Instances []string `json:"instances"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	if !validRole(b.Role) {
		fail(w, 400, "invalid_role")
		return
	}
	tok := randToken(24)
	inst, _ := json.Marshal(b.Instances)
	_, err := db.Exec(`INSERT INTO invites(token_hash,role,instances,created_by,expires_at) VALUES(?,?,?,?,?)`,
		sha(tok), b.Role, string(inst), u.Username, time.Now().Add(7*24*time.Hour).Unix())
	if err != nil {
		fail(w, 500, "db")
		return
	}
	audit(r, u.Username, "invite.create", b.Role)
	writeJSON(w, 200, map[string]string{"token": tok})
}

func handleInviteInfo(w http.ResponseWriter, r *http.Request) {
	var role string
	err := db.QueryRow(`SELECT role FROM invites WHERE token_hash=? AND used=0 AND expires_at>?`, sha(r.PathValue("token")), time.Now().Unix()).Scan(&role)
	if err != nil {
		fail(w, 404, "invite_invalid")
		return
	}
	writeJSON(w, 200, map[string]string{"role": role})
}

func handleInviteAccept(w http.ResponseWriter, r *http.Request) {
	var b struct{ Username, Password string }
	if !readJSON(w, r, &b) || !validCreds(w, b.Username, b.Password) {
		return
	}
	th := sha(r.PathValue("token"))
	tx, err := db.Begin()
	if err != nil {
		fail(w, 500, "db")
		return
	}
	defer tx.Rollback()
	var role, insts string
	// single use: claim atomically
	res, err := tx.Exec(`UPDATE invites SET used=1 WHERE token_hash=? AND used=0 AND expires_at>?`, th, time.Now().Unix())
	if n, _ := res.RowsAffected(); err != nil || n != 1 {
		fail(w, 404, "invite_invalid")
		return
	}
	_ = tx.QueryRow(`SELECT role,instances FROM invites WHERE token_hash=?`, th).Scan(&role, &insts)
	ur, err := tx.Exec(`INSERT INTO users(username,pass_hash,role,created_at) VALUES(?,?,?,?)`, b.Username, hashPassword(b.Password), role, time.Now().Unix())
	if err != nil {
		fail(w, 409, "username_taken")
		return
	}
	uid, _ := ur.LastInsertId()
	var ids []string
	_ = json.Unmarshal([]byte(insts), &ids)
	for _, id := range ids {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO user_instances(user_id,instance_id) VALUES(?,?)`, uid, id)
	}
	if err := tx.Commit(); err != nil {
		fail(w, 500, "db")
		return
	}
	startSession(w, r, uid)
	audit(r, b.Username, "invite.accept", role)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- users admin ----

func handleUsersList(w http.ResponseWriter, r *http.Request, u *User) {
	rows, err := db.Query(`SELECT id,username,role,created_at FROM users ORDER BY id`)
	if err != nil {
		fail(w, 500, "db")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, ts int64
		var name, role string
		_ = rows.Scan(&id, &name, &role, &ts)
		out = append(out, map[string]any{"id": id, "username": name, "role": role, "createdAt": ts, "instances": []string{}})
	}
	for _, o := range out {
		ir, _ := db.Query(`SELECT instance_id FROM user_instances WHERE user_id=?`, o["id"])
		list := []string{}
		for ir != nil && ir.Next() {
			var s string
			_ = ir.Scan(&s)
			list = append(list, s)
		}
		if ir != nil {
			ir.Close()
		}
		o["instances"] = list
	}
	writeJSON(w, 200, out)
}

func adminCount() int {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&n)
	return n
}

func handleUserUpdate(w http.ResponseWriter, r *http.Request, u *User) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var b struct {
		Role      string   `json:"role"`
		Instances []string `json:"instances"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	if !validRole(b.Role) {
		fail(w, 400, "invalid_role")
		return
	}
	var cur string
	if db.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&cur) != nil {
		fail(w, 404, "not_found")
		return
	}
	if cur == "admin" && b.Role != "admin" && adminCount() <= 1 {
		fail(w, 409, "last_admin")
		return
	}
	_, _ = db.Exec(`UPDATE users SET role=? WHERE id=?`, b.Role, id)
	_, _ = db.Exec(`DELETE FROM user_instances WHERE user_id=?`, id)
	for _, in := range b.Instances {
		_, _ = db.Exec(`INSERT OR IGNORE INTO user_instances(user_id,instance_id) VALUES(?,?)`, id, in)
	}
	audit(r, u.Username, "user.update", strconv.FormatInt(id, 10))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleUserDelete(w http.ResponseWriter, r *http.Request, u *User) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id == u.ID {
		fail(w, 409, "cannot_delete_self")
		return
	}
	_, _ = db.Exec(`DELETE FROM users WHERE id=?`, id)
	audit(r, u.Username, "user.delete", strconv.FormatInt(id, 10))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

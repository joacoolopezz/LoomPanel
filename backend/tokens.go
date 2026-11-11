package main

import (
	"net/http"
	"strings"
	"time"
)

// API tokens: "gp_" + 48 hex chars (24 bytes). Only the SHA-256 is stored; the token is shown once at creation.
// Prefix is first 12 chars ("gp_" + 9 hex) for better uniqueness in UI and audit logs.

func tokenUser(raw string) *User {
	if !strings.HasPrefix(raw, "gp_") || len(raw) != 51 {
		return nil
	}
	var u User
	var id string
	err := db.QueryRow(`SELECT t.id,u.id,u.username,u.role,t.scope FROM api_tokens t JOIN users u ON u.id=t.user_id
		WHERE t.token_hash=? AND (t.expires_at=0 OR t.expires_at>?)`, sha(raw), time.Now().Unix()).Scan(&id, &u.ID, &u.Username, &u.Role, &u.scope)
	if err != nil {
		return nil
	}
	now := time.Now().Unix()
	_, _ = db.Exec(`UPDATE api_tokens SET last_used=? WHERE id=? AND last_used<?`, now, id, now-60)
	u.viaToken = true
	return &u
}

func handleTokenList(w http.ResponseWriter, r *http.Request, u *User) {
	rows, err := db.Query(`SELECT id,name,prefix,scope,created_at,last_used,expires_at FROM api_tokens WHERE user_id=? ORDER BY created_at`, u.ID)
	if err != nil {
		fail(w, 500, "db")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, prefix, scope string
		var c, l, e int64
		_ = rows.Scan(&id, &name, &prefix, &scope, &c, &l, &e)
		out = append(out, map[string]any{"id": id, "name": name, "prefix": prefix, "scope": scope, "createdAt": c, "lastUsed": l, "expiresAt": e})
	}
	writeJSON(w, 200, out)
}

func handleTokenCreate(w http.ResponseWriter, r *http.Request, u *User) {
	if u.viaToken { // a token can't mint more tokens: only a real login can
		fail(w, 403, "forbidden")
		return
	}
	var b struct {
		Name        string `json:"name"`
		Scope       string `json:"scope"`
		ExpiresDays int    `json:"expiresDays"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" || len(b.Name) > 40 || (b.Scope != "full" && b.Scope != "read") || b.ExpiresDays < 0 || b.ExpiresDays > 365 {
		fail(w, 400, "invalid_input")
		return
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE user_id=?`, u.ID).Scan(&n)
	if n >= 20 {
		fail(w, 409, "too_many")
		return
	}
	raw := "gp_" + randToken(24) // 24 bytes = 48 hex chars, total 51 chars with prefix
	var exp int64
	if b.ExpiresDays > 0 {
		exp = time.Now().Add(time.Duration(b.ExpiresDays) * 24 * time.Hour).Unix()
	}
	id := randToken(6)
	_, err := db.Exec(`INSERT INTO api_tokens(id,user_id,name,prefix,token_hash,scope,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, u.ID, b.Name, raw[:12], sha(raw), b.Scope, time.Now().Unix(), exp)
	if err != nil {
		fail(w, 500, "db")
		return
	}
	audit(r, u.Username, "token.create", b.Name+" ("+b.Scope+")")
	writeJSON(w, 201, map[string]string{"id": id, "token": raw})
}

func handleTokenDelete(w http.ResponseWriter, r *http.Request, u *User) {
	_, _ = db.Exec(`DELETE FROM api_tokens WHERE id=? AND user_id=?`, r.PathValue("id"), u.ID)
	audit(r, u.Username, "token.delete", r.PathValue("id"))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

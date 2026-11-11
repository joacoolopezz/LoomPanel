package main

import (
	"net/http"
	"strconv"
	"time"
)

func audit(r *http.Request, user, action, target string) {
	ip := ""
	if r != nil {
		ip = clientIP(r)
	}
	_, _ = db.Exec(`INSERT INTO audit(ts,username,action,target,ip) VALUES(?,?,?,?,?)`, time.Now().Unix(), user, action, target, ip)
}

func handleAudit(w http.ResponseWriter, r *http.Request, u *User) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.Query(`SELECT ts,username,action,target,ip FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		fail(w, 500, "db")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ts int64
		var a, b, c, d string
		_ = rows.Scan(&ts, &a, &b, &c, &d)
		out = append(out, map[string]any{"ts": ts, "user": a, "action": b, "target": c, "ip": d})
	}
	writeJSON(w, 200, out)
}

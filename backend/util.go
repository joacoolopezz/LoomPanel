package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, 400, "invalid_json")
		return false
	}
	return true
}

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func clientIP(r *http.Request) string {
	if cfg.TrustProxy {
		if x := r.Header.Get("X-Forwarded-For"); x != "" {
			return strings.TrimSpace(strings.Split(x, ",")[0])
		}
	}
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		s = "server"
	}
	if len(s) > 30 {
		s = s[:30]
	}
	return s
}

// tiny in-memory rate limiter for login attempts
type limiter struct {
	mu sync.Mutex
	m  map[string][]time.Time
}

var loginLimiter = &limiter{m: map[string][]time.Time{}}

func init() {
	// Background cleanup for rate limiter map
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			loginLimiter.mu.Lock()
			cut := time.Now().Add(-30 * time.Minute)
			for k, v := range loginLimiter.m {
				var keep []time.Time
				for _, t := range v {
					if t.After(cut) {
						keep = append(keep, t)
					}
				}
				if len(keep) == 0 {
					delete(loginLimiter.m, k)
				} else {
					loginLimiter.m[k] = keep
				}
			}
			loginLimiter.mu.Unlock()
		}
	}()
}

func (l *limiter) tooMany(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-window)
	var keep []time.Time
	for _, t := range l.m[key] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	l.m[key] = keep
	return len(keep) >= max
}

func (l *limiter) hit(key string) {
	l.mu.Lock()
	l.m[key] = append(l.m[key], time.Now())
	l.mu.Unlock()
}

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func setup3(t *testing.T) {
	t.Helper()
	cfg = Config{DataDir: t.TempDir(), Lang: "es"}
	if err := openDB(cfg.DataDir); err != nil {
		t.Fatal(err)
	}
	if err := initSecrets(cfg.DataDir); err != nil {
		t.Fatal(err)
	}
	notifyClient = http.DefaultClient
}

func mkUser(t *testing.T, name, pw, role string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO users(username,pass_hash,role,created_at) VALUES(?,?,?,?)`, name, hashPassword(pw), role, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestTOTPVector(t *testing.T) {
	// RFC 6238 Appendix B, SHA-1, T=59s: 8-digit code 94287082 => 6-digit 287082
	if got := totpCode([]byte("12345678901234567890"), 59/30); got != "287082" {
		t.Fatalf("got %s want 287082", got)
	}
	if got := totpCode([]byte("12345678901234567890"), 1111111109/30); got != "081804" {
		t.Fatalf("got %s want 081804", got)
	}
}

func TestSecretsRoundTrip(t *testing.T) {
	setup3(t)
	s := seal([]byte("hunter2"))
	if strings.Contains(s, "hunter2") {
		t.Fatal("plaintext leaked")
	}
	if b, err := unseal(s); err != nil || string(b) != "hunter2" {
		t.Fatalf("unseal: %q %v", b, err)
	}
	if _, err := unseal(s[:len(s)-4] + "AAAA"); err == nil {
		t.Fatal("tampering must be detected")
	}
}

func loginReq(user, pw string) *http.Request {
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"`+user+`","password":"`+pw+`"}`))
	return r
}

func TestTwoFactorLoginFlow(t *testing.T) {
	setup3(t)
	uid := mkUser(t, "alice", "correct-horse-1", "admin")
	secretB32 := "JBSWY3DPEHPK3PXP"
	_, _ = db.Exec(`UPDATE users SET totp_secret=?, totp_enabled=1 WHERE id=?`, secretB32, uid)
	secret, _ := b32.DecodeString(secretB32)
	codeNow := func() string { return totpCode(secret, time.Now().Unix()/30) }

	step1 := func() (ticket string) {
		w := httptest.NewRecorder()
		handleLogin(w, loginReq("alice", "correct-horse-1"))
		var out struct {
			Needs2FA bool   `json:"needs2fa"`
			Ticket   string `json:"ticket"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if !out.Needs2FA || out.Ticket == "" {
			t.Fatalf("expected 2fa challenge, got %s", w.Body.String())
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("no session may be issued before the second factor")
		}
		return out.Ticket
	}
	second := func(ticket, code string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handleLogin2FA(w, httptest.NewRequest("POST", "/api/login/2fa", strings.NewReader(`{"ticket":"`+ticket+`","code":"`+code+`"}`)))
		return w
	}

	tk := step1()
	if w := second(tk, "000000"); w.Code != 401 {
		t.Errorf("wrong code: %d", w.Code)
	}
	w := second(tk, codeNow())
	if w.Code != 200 || len(w.Result().Cookies()) == 0 {
		t.Fatalf("right code should log in: %d %s", w.Code, w.Body.String())
	}
	if w := second(tk, codeNow()); w.Code != 401 {
		t.Error("ticket must be single use")
	}
	// the same TOTP window cannot be replayed
	if w := second(step1(), codeNow()); w.Code != 401 {
		t.Errorf("replayed code accepted: %d", w.Code)
	}
	// recovery codes work exactly once
	codes := newRecoveryCodes(uid)
	if w := second(step1(), codes[0]); w.Code != 200 {
		t.Errorf("recovery code rejected: %d", w.Code)
	}
	if w := second(step1(), codes[0]); w.Code != 401 {
		t.Error("recovery code reused")
	}
	// brute force: ticket dies after 5 wrong attempts
	tk = step1()
	for i := 0; i < 5; i++ {
		second(tk, "111111")
	}
	if w := second(tk, "222222"); w.Code != 429 {
		t.Errorf("expected lockout, got %d", w.Code)
	}
}

func TestAPITokens(t *testing.T) {
	setup3(t)
	uid := mkUser(t, "bot", "pw-pw-pw-pw-1", "admin")
	mk := func(scope string) string {
		raw := "gp_" + randToken(20)
		_, _ = db.Exec(`INSERT INTO api_tokens(id,user_id,name,prefix,token_hash,scope,created_at) VALUES(?,?,?,?,?,?,?)`, randToken(4), uid, "t", raw[:7], sha(raw), scope, 1)
		return raw
	}
	h := withAuth("", func(w http.ResponseWriter, r *http.Request, u *User) { writeJSON(w, 200, map[string]string{"user": u.Username}) })
	do := func(method, tok string, hdr map[string]string) int {
		r := httptest.NewRequest(method, "/x", nil)
		if tok != "" {
			r.Header.Set("Authorization", "Bearer "+tok)
		}
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}
	full, ro := mk("full"), mk("read")
	if do("GET", full, nil) != 200 || do("POST", full, nil) != 200 {
		t.Error("full token should read and write without the CSRF header")
	}
	if do("GET", ro, nil) != 200 || do("POST", ro, nil) != 403 {
		t.Error("read-only token must not write")
	}
	if do("GET", "gp_"+strings.Repeat("0", 40), nil) != 401 || do("GET", "garbage", nil) != 401 {
		t.Error("bad tokens must be rejected")
	}
	if do("POST", "", nil) != 401 {
		t.Error("anonymous must be rejected")
	}
	// a token cannot mint new tokens
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/tokens", strings.NewReader(`{"name":"x","scope":"full"}`))
	handleTokenCreate(w, r, &User{ID: uid, Username: "bot", Role: "admin", viaToken: true})
	if w.Code != 403 {
		t.Errorf("token minting via token: %d", w.Code)
	}
	// expired token
	raw := "gp_" + randToken(20)
	_, _ = db.Exec(`INSERT INTO api_tokens(id,user_id,name,prefix,token_hash,scope,created_at,expires_at) VALUES('e',?,?,?,?,?,?,?)`, uid, "t", raw[:7], sha(raw), "full", 1, 5)
	if do("GET", raw, nil) != 401 {
		t.Error("expired token accepted")
	}
}

func TestNotifications(t *testing.T) {
	setup3(t)
	var mu sync.Mutex
	var got []map[string]any
	var sig, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		got = append(got, m)
		sig, path = r.Header.Get("X-LoomPanel-Signature")+"|"+hex.EncodeToString(b), r.URL.Path
		mu.Unlock()
	}))
	defer srv.Close()
	telegramBase = srv.URL
	add := func(typ string, events []string, cf map[string]string) {
		c, _ := json.Marshal(cf)
		e, _ := json.Marshal(events)
		_, _ = db.Exec(`INSERT INTO notif_channels(id,type,name,summary,events,config) VALUES(?,?,?,?,?,?)`, randToken(4), typ, typ, "", string(e), seal(c))
	}
	add("webhook", []string{"server.crashed"}, map[string]string{"url": srv.URL + "/hook", "secret": "s3cret"})
	add("telegram", []string{"server.crashed", "backup.failed"}, map[string]string{"token": "123:abc", "chatId": "42"})

	notify("server.started", "Survival") // nobody subscribed: nothing may be sent
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("unsubscribed event delivered: %v", got)
	}
	mu.Unlock()

	notify("server.crashed", "Survival")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(got))
	}
	var sawText, sawTG bool
	for _, m := range got {
		if s, _ := m["message"].(string); strings.Contains(s, "Survival") && strings.Contains(s, "cayó") {
			sawText = true
		}
		if s, _ := m["text"].(string); strings.Contains(s, "Survival") && m["chat_id"] == "42" {
			sawTG = true
		}
	}
	if !sawText || !sawTG {
		t.Errorf("payloads wrong: %v", got)
	}
	_ = sig
	_ = path
}

func TestWebhookSignature(t *testing.T) {
	setup3(t)
	var gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-LoomPanel-Signature")
		gotBody, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()
	err := sendNotification(channel{Type: "webhook", Config: map[string]string{"url": srv.URL, "secret": "k"}}, "backup.completed", "Srv")
	if err != nil {
		t.Fatal(err)
	}
	m := hmac.New(sha256.New, []byte("k"))
	m.Write(gotBody)
	if gotSig != "sha256="+hex.EncodeToString(m.Sum(nil)) {
		t.Errorf("signature mismatch: %s", gotSig)
	}
}

func TestClassifyExit(t *testing.T) {
	cases := []struct {
		exp  bool
		code int
		want string
	}{{false, 0, "server.stopped"}, {false, 1, "server.crashed"}, {false, 137, "server.crashed"}, {true, 143, "server.stopped"}, {true, 137, "server.stopped"}}
	for _, c := range cases {
		if got := classifyExit(c.exp, c.code); got != c.want {
			t.Errorf("classify(%v,%d)=%s want %s", c.exp, c.code, got, c.want)
		}
	}
	markExpected("x")
	if !consumeExpected("x") || consumeExpected("x") {
		t.Error("expected flag must be consumed exactly once")
	}
}

func TestCloudflareRecords(t *testing.T) {
	setup3(t)
	type call struct{ Method, Path string; Body map[string]any }
	var calls []call
	records := map[string]string{} // key "TYPE|name" -> id
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, call{r.Method, r.URL.Path, body})
		if r.Header.Get("Authorization") != "Bearer TOKEN-TOKEN-TOKEN-TOKEN" {
			w.WriteHeader(403)
			io.WriteString(w, `{"success":false,"errors":[{"message":"bad token"}],"result":null}`)
			return
		}
		ok := func(v string) { io.WriteString(w, `{"success":true,"errors":[],"result":`+v+`}`) }
		switch {
		case r.URL.Path == "/user/tokens/verify":
			ok(`{}`)
		case r.URL.Path == "/zones":
			if r.URL.Query().Get("name") == "example.com" {
				ok(`[{"id":"z1"}]`)
			} else {
				ok(`[]`)
			}
		case strings.HasSuffix(r.URL.Path, "/dns_records") && r.Method == "GET":
			if id := records[r.URL.Query().Get("type")+"|"+r.URL.Query().Get("name")]; id != "" {
				ok(`[{"id":"` + id + `"}]`)
			} else {
				ok(`[]`)
			}
		case strings.HasSuffix(r.URL.Path, "/dns_records") && r.Method == "POST":
			records[body["type"].(string)+"|"+body["name"].(string)] = "rec-" + body["type"].(string)
			ok(`{"id":"new"}`)
		case r.Method == "PUT":
			ok(`{"id":"upd"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cfBase = srv.URL
	setSetting("cloudflare_token", seal([]byte("TOKEN-TOKEN-TOKEN-TOKEN")))
	ipMu.Lock()
	ipCache, ipFetched = "203.0.113.9", time.Now()
	ipMu.Unlock()

	in := Instance{ID: "i", Name: "n", Port: 25566, Domain: "mc.example.com"}
	run := func() int {
		w := httptest.NewRecorder()
		handleDomainCloudflare(w, httptest.NewRequest("POST", "/", nil), &User{Username: "a"}, in)
		return w.Code
	}
	if c := run(); c != 200 {
		t.Fatalf("first apply: %d", c)
	}
	var aBody, srvBody map[string]any
	for _, c := range calls {
		if c.Method == "POST" && c.Body != nil {
			if c.Body["type"] == "A" {
				aBody = c.Body
			}
			if c.Body["type"] == "SRV" {
				srvBody = c.Body
			}
		}
	}
	if aBody == nil || aBody["content"] != "203.0.113.9" || aBody["proxied"] != false {
		t.Errorf("A record wrong (must be DNS-only): %v", aBody)
	}
	d, _ := srvBody["data"].(map[string]any)
	if srvBody == nil || srvBody["name"] != "_minecraft._tcp.mc.example.com" || d["port"] != float64(25566) || d["target"] != "mc.example.com" {
		t.Errorf("SRV record wrong: %v", srvBody)
	}
	calls = nil
	if c := run(); c != 200 { // second run updates instead of duplicating
		t.Fatalf("second apply: %d", c)
	}
	puts, posts := 0, 0
	for _, c := range calls {
		if c.Method == "PUT" {
			puts++
		}
		if c.Method == "POST" {
			posts++
		}
	}
	if puts != 2 || posts != 0 {
		t.Errorf("idempotency: puts=%d posts=%d", puts, posts)
	}
	// default port needs no SRV
	in.Port = 25565
	calls = nil
	run()
	for _, c := range calls {
		if c.Body != nil && c.Body["type"] == "SRV" {
			t.Error("no SRV expected on the default port")
		}
	}
	if len(recordsFor(Instance{Domain: "a.b.com", Port: 25565}, "1.1.1.1")) != 1 || len(recordsFor(Instance{Domain: "a.b.com", Port: 25570}, "1.1.1.1")) != 2 {
		t.Error("recordsFor SRV logic")
	}
	// token verification on save
	w := httptest.NewRecorder()
	handleCFSet(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"token":"WRONG-WRONG-WRONG-WRONG"}`)), &User{Username: "a"})
	if w.Code != 400 {
		t.Errorf("invalid CF token accepted: %d", w.Code)
	}
	if !hostRe.MatchString("mc.example.com") || hostRe.MatchString("bad_host") || hostRe.MatchString("localhost") || hostRe.MatchString("a.com/evil") {
		t.Error("hostRe")
	}
}

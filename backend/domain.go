package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ipifyURL = "https://api.ipify.org"
	cfBase   = "https://api.cloudflare.com/client/v4"
	hostRe   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

	ipMu      sync.Mutex
	ipCache   string
	ipFetched time.Time
)

// publicIPCached returns the last known public IP, refreshing it if stale.
// If cache is expired, it fetches synchronously (with timeout) to avoid returning stale IP.
func publicIPCached() string {
	ipMu.Lock()
	needRefresh := time.Since(ipFetched) > time.Hour
	ipMu.Unlock()

	if needRefresh {
		// Fetch fresh IP synchronously with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", ipifyURL, nil)
		resp, err := httpc.Do(req)
		if err == nil {
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
			if ip := net.ParseIP(strings.TrimSpace(string(b))); ip != nil {
				ipMu.Lock()
				ipCache, ipFetched = ip.String(), time.Now()
				ipMu.Unlock()
			}
		}
	}

	ipMu.Lock()
	defer ipMu.Unlock()
	return ipCache
}

type dnsRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Note  string `json:"note"`
}

func recordsFor(in Instance, ip string) []dnsRecord {
	if in.Domain == "" {
		return nil
	}
	if ip == "" {
		ip = "<IP-DE-TU-SERVIDOR>"
	}
	recs := []dnsRecord{{"A", in.Domain, ip, "dns_a"}}
	if in.Port != 25565 { // players can omit the port only if an SRV record points to it
		recs = append(recs, dnsRecord{"SRV", "_minecraft._tcp." + in.Domain, fmt.Sprintf("0 5 %d %s", in.Port, in.Domain), "dns_srv"})
	}
	return recs
}

func handleDomainGet(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	ip := publicIPCached()
	writeJSON(w, 200, map[string]any{"hostname": in.Domain, "port": in.Port, "publicIp": ip, "records": recordsFor(in, ip), "cloudflare": getSetting("cloudflare_token") != ""})
}

func handleDomainSet(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ Hostname string }
	if !readJSON(w, r, &b) {
		return
	}
	h := strings.ToLower(strings.TrimSpace(b.Hostname))
	if h != "" && !hostRe.MatchString(h) {
		fail(w, 400, "invalid_hostname")
		return
	}
	_, _ = db.Exec(`UPDATE instances SET domain=? WHERE id=?`, h, in.ID)
	audit(r, u.Username, "domain.set", in.Name+": "+h)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleDomainCheck lets people learn DNS by doing: it reports what the world currently resolves.
func handleDomainCheck(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	if in.Domain == "" {
		fail(w, 400, "no_domain")
		return
	}
	ip := publicIPCached()
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	addrs, aerr := net.DefaultResolver.LookupHost(ctx, in.Domain)
	aOK := false
	for _, a := range addrs {
		aOK = aOK || (ip != "" && a == ip)
	}
	res := map[string]any{"a": map[string]any{"ok": aOK, "found": addrs, "error": errStr(aerr) != "", "expected": ip}}
	srvNeeded := in.Port != 25565
	srv := map[string]any{"needed": srvNeeded, "ok": !srvNeeded}
	if srvNeeded {
		_, recs, err := net.DefaultResolver.LookupSRV(ctx, "minecraft", "tcp", in.Domain)
		found := []string{}
		for _, s := range recs {
			found = append(found, fmt.Sprintf("%d %s", s.Port, strings.TrimSuffix(s.Target, ".")))
			if int(s.Port) == in.Port {
				srv["ok"] = true
			}
		}
		srv["found"], srv["error"] = found, err != nil
	}
	res["srv"] = srv
	writeJSON(w, 200, res)
}

// ---- Cloudflare ----

func cfToken() string {
	if v := getSetting("cloudflare_token"); v != "" {
		if b, err := unseal(v); err == nil {
			return string(b)
		}
	}
	return ""
}

func cfDo(token, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, cfBase+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env struct {
		Success bool                       `json:"success"`
		Errors  []struct{ Message string } `json:"errors"`
		Result  json.RawMessage            `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&env); err != nil {
		return fmt.Errorf("cloudflare: HTTP %d", resp.StatusCode)
	}
	if !env.Success {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if len(env.Errors) > 0 {
			msg = env.Errors[0].Message
		}
		return errors.New("cloudflare: " + msg)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func cfZone(token, host string) (string, error) {
	labels := strings.Split(host, ".")
	for i := 0; i < len(labels)-1; i++ {
		var zs []struct {
			ID string `json:"id"`
		}
		if err := cfDo(token, "GET", "/zones?name="+url.QueryEscape(strings.Join(labels[i:], ".")), nil, &zs); err != nil {
			return "", err
		}
		if len(zs) > 0 {
			return zs[0].ID, nil
		}
	}
	return "", errors.New("zone_not_found")
}

func cfUpsert(token, zone, typ, name string, body map[string]any) error {
	var existing []struct {
		ID string `json:"id"`
	}
	if err := cfDo(token, "GET", "/zones/"+zone+"/dns_records?type="+typ+"&name="+url.QueryEscape(name), nil, &existing); err != nil {
		return err
	}
	if len(existing) > 0 {
		return cfDo(token, "PUT", "/zones/"+zone+"/dns_records/"+existing[0].ID, body, nil)
	}
	return cfDo(token, "POST", "/zones/"+zone+"/dns_records", body, nil)
}

func handleIntegrations(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, 200, map[string]bool{"cloudflare": getSetting("cloudflare_token") != ""})
}

func handleCFSet(w http.ResponseWriter, r *http.Request, u *User) {
	var b struct{ Token string }
	if !readJSON(w, r, &b) {
		return
	}
	b.Token = strings.TrimSpace(b.Token)
	if len(b.Token) < 20 || len(b.Token) > 200 || strings.ContainsAny(b.Token, " \r\n") {
		fail(w, 400, "invalid_input")
		return
	}
	if err := cfDo(b.Token, "GET", "/user/tokens/verify", nil, nil); err != nil {
		fail(w, 400, "cloudflare_token_invalid")
		return
	}
	setSetting("cloudflare_token", seal([]byte(b.Token)))
	audit(r, u.Username, "integration.cloudflare_set", "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleCFClear(w http.ResponseWriter, r *http.Request, u *User) {
	_, _ = db.Exec(`DELETE FROM settings WHERE key='cloudflare_token'`)
	audit(r, u.Username, "integration.cloudflare_clear", "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleDomainCloudflare(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	token := cfToken()
	if token == "" {
		fail(w, 409, "cloudflare_not_configured")
		return
	}
	if in.Domain == "" {
		fail(w, 400, "no_domain")
		return
	}
	ip := publicIPCached()
	if ip == "" {
		fail(w, 502, "public_ip_unknown")
		return
	}
	zone, err := cfZone(token, in.Domain)
	if err != nil {
		code := "cloudflare_failed"
		if err.Error() == "zone_not_found" {
			code = err.Error()
		}
		fail(w, 502, code)
		return
	}
	// Minecraft is not HTTP: the record must be DNS-only (proxied=false) or players cannot connect.
	if err := cfUpsert(token, zone, "A", in.Domain, map[string]any{"type": "A", "name": in.Domain, "content": ip, "ttl": 1, "proxied": false}); err != nil {
		fail(w, 502, "cloudflare_failed")
		return
	}
	if in.Port != 25565 {
		srvName := "_minecraft._tcp." + in.Domain
		err := cfUpsert(token, zone, "SRV", srvName, map[string]any{"type": "SRV", "name": srvName, "ttl": 1,
			"data": map[string]any{"service": "_minecraft", "proto": "_tcp", "name": in.Domain, "priority": 0, "weight": 5, "port": in.Port, "target": in.Domain}})
		if err != nil {
			fail(w, 502, "cloudflare_failed")
			return
		}
	}
	audit(r, u.Username, "domain.cloudflare", in.Name+": "+in.Domain)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

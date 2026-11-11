package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Minimal 5-field cron: minute hour day-of-month month day-of-week.
// Supports *, lists (1,2), ranges (1-5) and steps (*/10, 1-30/5).
type cronSpec struct {
	min, hour, dom, mon, dow uint64
	domStar, dowStar         bool
}

func parseField(f string, lo, hi int) (uint64, bool, error) {
	var bits uint64
	star := f == "*"
	for _, part := range strings.Split(f, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n < 1 {
				return 0, false, errors.New("bad step")
			}
			step, part = n, part[:i]
		}
		a, b := lo, hi
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			p := strings.SplitN(part, "-", 2)
			x, e1 := strconv.Atoi(p[0])
			y, e2 := strconv.Atoi(p[1])
			if e1 != nil || e2 != nil {
				return 0, false, errors.New("bad range")
			}
			a, b = x, y
		default:
			x, err := strconv.Atoi(part)
			if err != nil {
				return 0, false, errors.New("bad value")
			}
			a, b = x, x
			// Single value with step (e.g., "5/2") should be treated as just that value,
			// not expanded to the full range. Step only applies to ranges or *.
		}
		if a < lo || b > hi || a > b {
			return 0, false, errors.New("out of range")
		}
		for v := a; v <= b; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, star, nil
}

func parseCron(s string) (*cronSpec, error) {
	f := strings.Fields(s)
	if len(f) != 5 {
		return nil, errors.New("cron needs 5 fields")
	}
	var c cronSpec
	var err error
	if c.min, _, err = parseField(f[0], 0, 59); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if c.hour, _, err = parseField(f[1], 0, 23); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if c.dom, c.domStar, err = parseField(f[2], 1, 31); err != nil {
		return nil, fmt.Errorf("day: %w", err)
	}
	if c.mon, _, err = parseField(f[3], 1, 12); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	if c.dow, c.dowStar, err = parseField(f[4], 0, 7); err != nil {
		return nil, fmt.Errorf("weekday: %w", err)
	}
	if c.dow&(1<<7) != 0 { // 7 is Sunday too
		c.dow |= 1
	}
	return &c, nil
}

func (c *cronSpec) match(t time.Time) bool {
	if c.min&(1<<uint(t.Minute())) == 0 || c.hour&(1<<uint(t.Hour())) == 0 || c.mon&(1<<uint(t.Month())) == 0 {
		return false
	}
	d := c.dom&(1<<uint(t.Day())) != 0
	w := c.dow&(1<<uint(t.Weekday())) != 0
	if !c.domStar && !c.dowStar { // classic cron: either may match when both are restricted
		return d || w
	}
	return d && w
}

type schedule struct {
	ID, InstanceID, Name, Cron, Action, Payload string
	Enabled                                     bool
	LastRun                                     int64
}

var scheduleActions = map[string]bool{"start": true, "stop": true, "restart": true, "command": true, "backup": true}

func startScheduler() {
	go func() {
		for {
			next := time.Now().Truncate(time.Minute).Add(time.Minute)
			time.Sleep(time.Until(next))
			runDue(next)
		}
	}()
}

func runDue(t time.Time) {
	rows, err := db.Query(`SELECT id,instance_id,name,cron,action,payload FROM schedules WHERE enabled=1`)
	if err != nil {
		return
	}
	var due []schedule
	for rows.Next() {
		var s schedule
		_ = rows.Scan(&s.ID, &s.InstanceID, &s.Name, &s.Cron, &s.Action, &s.Payload)
		if c, err := parseCron(s.Cron); err == nil && c.match(t) {
			due = append(due, s)
		}
	}
	rows.Close()
	for _, s := range due {
		go runSchedule(s)
	}
}

func runSchedule(s schedule) {
	in, ok := getInstance(s.InstanceID)
	if !ok {
		return
	}
	st := statusOf(in)
	note := ""
	switch s.Action {
	case "start":
		if st == "offline" {
			note = errStr(startInstance(in))
		}
	case "stop":
		if st == "running" || st == "starting" {
			stopInstance(in)
		}
	case "restart":
		if st == "running" || st == "starting" {
			stopInstance(in)
			note = errStr(startInstance(in))
		}
	case "command":
		if st == "running" {
			note = errStr(sendCommandContext(context.Background(), in.ID, s.Payload))
		}
	case "backup":
		if _, err := createBackup(in, "scheduled"); err != nil {
			note = err.Error()
		} else {
			keep, _ := strconv.Atoi(s.Payload)
			if keep < 1 {
				keep = 7
			}
			pruneBackups(in.ID, "scheduled", keep)
		}
	}
	_, _ = db.Exec(`UPDATE schedules SET last_run=? WHERE id=?`, time.Now().Unix(), s.ID)
	audit(nil, "scheduler", "schedule."+s.Action, in.Name+": "+s.Name+truncate(" "+note, 80))
}

func errStr(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

func handleScheduleList(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	rows, err := db.Query(`SELECT id,name,cron,action,payload,enabled,last_run FROM schedules WHERE instance_id=? ORDER BY rowid`, in.ID)
	if err != nil {
		fail(w, 500, "db")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, cron, action, payload string
		var en int
		var last int64
		_ = rows.Scan(&id, &name, &cron, &action, &payload, &en, &last)
		out = append(out, map[string]any{"id": id, "name": name, "cron": cron, "action": action, "payload": payload, "enabled": en == 1, "lastRun": last})
	}
	writeJSON(w, 200, out)
}

func handleScheduleCreate(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ Name, Cron, Action, Payload string }
	if !readJSON(w, r, &b) {
		return
	}
	b.Name, b.Cron = strings.TrimSpace(b.Name), strings.Join(strings.Fields(b.Cron), " ")
	if b.Name == "" || len(b.Name) > 60 || !scheduleActions[b.Action] {
		fail(w, 400, "invalid_input")
		return
	}
	if _, err := parseCron(b.Cron); err != nil {
		fail(w, 400, "invalid_cron")
		return
	}
	switch b.Action {
	case "command":
		b.Payload = strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ").Replace(b.Payload))
		if b.Payload == "" || len(b.Payload) > 500 {
			fail(w, 400, "invalid_input")
			return
		}
	case "backup":
		if n, err := strconv.Atoi(b.Payload); err != nil || n < 1 || n > 100 {
			b.Payload = "7"
		}
	default:
		b.Payload = ""
	}
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM schedules WHERE instance_id=?`, in.ID).Scan(&count)
	if count >= 50 {
		fail(w, 409, "too_many")
		return
	}
	id := randToken(6)
	_, err := db.Exec(`INSERT INTO schedules(id,instance_id,name,cron,action,payload) VALUES(?,?,?,?,?,?)`, id, in.ID, b.Name, b.Cron, b.Action, b.Payload)
	if err != nil {
		fail(w, 500, "db")
		return
	}
	audit(r, u.Username, "schedule.create", in.Name+": "+b.Name)
	writeJSON(w, 201, map[string]string{"id": id})
}

func handleScheduleToggle(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ Enabled bool }
	if !readJSON(w, r, &b) {
		return
	}
	en := 0
	if b.Enabled {
		en = 1
	}
	_, _ = db.Exec(`UPDATE schedules SET enabled=? WHERE id=? AND instance_id=?`, en, r.PathValue("sid"), in.ID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleScheduleDelete(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	_, _ = db.Exec(`DELETE FROM schedules WHERE id=? AND instance_id=?`, r.PathValue("sid"), in.ID)
	audit(r, u.Username, "schedule.delete", in.Name)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

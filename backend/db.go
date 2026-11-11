package main

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

const schema = `
CREATE TABLE IF NOT EXISTS users(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT UNIQUE NOT NULL,
  pass_hash TEXT NOT NULL,
  role TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions(
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS invites(
  token_hash TEXT PRIMARY KEY,
  role TEXT NOT NULL,
  instances TEXT NOT NULL DEFAULT '[]',
  created_by TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  used INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS user_instances(
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  instance_id TEXT NOT NULL,
  PRIMARY KEY(user_id, instance_id)
);
CREATE TABLE IF NOT EXISTS instances(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  slug TEXT UNIQUE NOT NULL,
  game TEXT NOT NULL DEFAULT 'minecraft',
  type TEXT NOT NULL,
  version TEXT NOT NULL,
  java INTEGER NOT NULL,
  ram_mb INTEGER NOT NULL,
  port INTEGER NOT NULL,
  network TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'installing',
  message TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS backups(
  id TEXT PRIMARY KEY,
  instance_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  size INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS schedules(
  id TEXT PRIMARY KEY,
  instance_id TEXT NOT NULL,
  name TEXT NOT NULL,
  cron TEXT NOT NULL,
  action TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  last_run INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS api_tokens(
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  token_hash TEXT UNIQUE NOT NULL,
  scope TEXT NOT NULL DEFAULT 'full',
  created_at INTEGER NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS totp_recovery(
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash TEXT NOT NULL,
  PRIMARY KEY(user_id, code_hash)
);
CREATE TABLE IF NOT EXISTS notif_channels(
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  name TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  events TEXT NOT NULL DEFAULT '[]',
  enabled INTEGER NOT NULL DEFAULT 1,
  config TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  username TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT ''
);
`

func openDB(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	d, err := sql.Open("sqlite3", "file:"+filepath.Join(dir, "panel.db")+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return err
	}
	d.SetMaxOpenConns(4)
	if _, err = d.Exec(schema); err != nil {
		return err
	}
	_, _ = d.Exec(`ALTER TABLE instances ADD COLUMN launch TEXT NOT NULL DEFAULT ''`) // phase-2 migration; errors if it already exists
	for _, m := range []string{                                                       // phase-3 migrations; each errors harmlessly if the column already exists
		`ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN totp_last INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE instances ADD COLUMN domain TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = d.Exec(m)
	}
	db = d
	return nil
}

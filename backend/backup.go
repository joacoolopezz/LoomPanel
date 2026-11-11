package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	bkMu   sync.Mutex
	bkBusy = map[string]bool{}
)

func backupDir(id string) string { return filepath.Join(cfg.DataDir, "backups", id) }
func backupBusy(id string) bool  { bkMu.Lock(); defer bkMu.Unlock(); return bkBusy[id] }

func lockBackup(id string) bool {
	bkMu.Lock()
	defer bkMu.Unlock()
	if bkBusy[id] {
		return false
	}
	bkBusy[id] = true
	return true
}
func unlockBackup(id string) { bkMu.Lock(); delete(bkBusy, id); bkMu.Unlock() }

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func writeTar(src, dest string) (int64, error) {
	f, err := os.Create(dest + ".part")
	if err != nil {
		return 0, err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil || !(info.Mode().IsRegular() || info.IsDir()) {
			return nil // symlinks and special files are skipped
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
		if info.IsDir() {
			hdr.Name += "/"
			return tw.WriteHeader(hdr)
		}
		in, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer in.Close()
		st, err := in.Stat()
		if err != nil {
			return nil
		}
		hdr.Size = st.Size()
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		n, _ := io.Copy(tw, io.LimitReader(in, hdr.Size))
		if n < hdr.Size { // file shrank while being read: pad so the archive stays valid
			_, err = io.Copy(tw, io.LimitReader(zeros{}, hdr.Size-n))
		}
		return err
	})
	if e := tw.Close(); err == nil {
		err = e
	}
	if e := gz.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		os.Remove(dest + ".part")
		return 0, err
	}
	if err := os.Rename(dest+".part", dest); err != nil {
		return 0, err
	}
	st, _ := os.Stat(dest)
	return st.Size(), nil
}

func extractTar(file, dst string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(path.Clean("/"+h.Name), "/")
		if name == "" {
			continue
		}
		dest := filepath.Join(dst, filepath.FromSlash(name))
		if !strings.HasPrefix(dest, dst+string(os.PathSeparator)) {
			return errors.New("unsafe path in archive")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			// Preserve original file permissions from archive
			mode := fs.FileMode(h.Mode & 0o777)
			if mode == 0 {
				mode = 0o644 // sensible default if archive has no perms
			}
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, io.LimitReader(tr, h.Size))
			out.Close()
			if err != nil {
				return err
			}
		}
	}
}

// doBackup assumes the per-instance backup lock is held.
func doBackup(in Instance, kind string) (string, error) {
	if statusOf(in) == "running" { // ask the game to flush and pause world writes for a consistent copy
		ctx := context.Background()
		_ = sendCommandContext(ctx, in.ID, "save-off")
		_ = sendCommandContext(ctx, in.ID, "save-all flush")
		time.Sleep(4 * time.Second)
		defer sendCommandContext(ctx, in.ID, "save-on")
	}
	if err := os.MkdirAll(backupDir(in.ID), 0o755); err != nil {
		return "", err
	}
	id := randToken(6)
	size, err := writeTar(instDir(in.ID), filepath.Join(backupDir(in.ID), id+".tar.gz"))
	if err != nil {
		return "", err
	}
	_, err = db.Exec(`INSERT INTO backups(id,instance_id,kind,size,created_at) VALUES(?,?,?,?,?)`, id, in.ID, kind, size, time.Now().Unix())
	return id, err
}

func createBackup(in Instance, kind string) (string, error) {
	if !lockBackup(in.ID) {
		return "", errors.New("backup_busy")
	}
	defer unlockBackup(in.ID)
	id, err := doBackup(in, kind)
	if kind != "pre-restore" {
		if err != nil {
			go notify("backup.failed", in.Name)
		} else {
			go notify("backup.completed", in.Name)
		}
	}
	return id, err
}

// pruneBackups keeps the newest `keep` backups of a kind and deletes the rest.
func pruneBackups(instanceID, kind string, keep int) {
	rows, err := db.Query(`SELECT id FROM backups WHERE instance_id=? AND kind=? ORDER BY created_at DESC, rowid DESC`, instanceID, kind)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for i, id := range ids {
		if i >= keep {
			_ = os.Remove(filepath.Join(backupDir(instanceID), id+".tar.gz"))
			_, _ = db.Exec(`DELETE FROM backups WHERE id=?`, id)
		}
	}
}

func restoreBackup(in Instance, bid string) error {
	if !lockBackup(in.ID) {
		return errors.New("backup_busy")
	}
	defer unlockBackup(in.ID)
	file := filepath.Join(backupDir(in.ID), bid+".tar.gz")
	if _, err := os.Stat(file); err != nil {
		return err
	}
	if _, err := doBackup(in, "pre-restore"); err != nil { // always keep a way back
		return err
	}
	pruneBackups(in.ID, "pre-restore", 3)
	dir := instDir(in.ID)
	tmp, old := dir+".restore", dir+".old"
	_ = os.RemoveAll(tmp)
	_ = os.RemoveAll(old)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	if err := extractTar(file, tmp); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(dir, old); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.Rename(old, dir)
		return err
	}
	return os.RemoveAll(old)
}

func lookupBackup(w http.ResponseWriter, in Instance, bid string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM backups WHERE id=? AND instance_id=?`, bid, in.ID).Scan(&n)
	if n == 0 {
		fail(w, 404, "not_found")
	}
	return n > 0
}

func handleBackupList(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	rows, err := db.Query(`SELECT id,kind,size,created_at FROM backups WHERE instance_id=? ORDER BY created_at DESC, rowid DESC`, in.ID)
	if err != nil {
		fail(w, 500, "db")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, kind string
		var size, ts int64
		_ = rows.Scan(&id, &kind, &size, &ts)
		items = append(items, map[string]any{"id": id, "kind": kind, "size": size, "createdAt": ts})
	}
	writeJSON(w, 200, map[string]any{"busy": backupBusy(in.ID), "items": items})
}

func handleBackupCreate(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	if backupBusy(in.ID) {
		fail(w, 409, "backup_busy")
		return
	}
	audit(r, u.Username, "backup.create", in.Name)
	go func() {
		if _, err := createBackup(in, "manual"); err != nil {
			audit(nil, "system", "backup.failed", in.Name+": "+truncate(err.Error(), 80))
		}
	}()
	writeJSON(w, 202, map[string]bool{"ok": true})
}

func handleBackupDownload(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	bid := r.PathValue("bid")
	if !lookupBackup(w, in, bid) {
		return
	}
	f, err := os.Open(filepath.Join(backupDir(in.ID), bid+".tar.gz"))
	if err != nil {
		fail(w, 404, "not_found")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Disposition", `attachment; filename="`+in.Slug+`-`+bid+`.tar.gz"`)
	http.ServeContent(w, r, bid+".tar.gz", st.ModTime(), f)
}

func handleBackupRestore(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	bid := r.PathValue("bid")
	if !lookupBackup(w, in, bid) {
		return
	}
	if statusOf(in) != "offline" {
		fail(w, 409, "must_be_offline")
		return
	}
	if backupBusy(in.ID) {
		fail(w, 409, "backup_busy")
		return
	}
	audit(r, u.Username, "backup.restore", in.Name+": "+bid)
	go func() {
		if err := restoreBackup(in, bid); err != nil {
			audit(nil, "system", "backup.restore_failed", in.Name+": "+truncate(err.Error(), 80))
		}
	}()
	writeJSON(w, 202, map[string]bool{"ok": true})
}

func handleBackupDelete(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	bid := r.PathValue("bid")
	if !lookupBackup(w, in, bid) {
		return
	}
	_ = os.Remove(filepath.Join(backupDir(in.ID), bid+".tar.gz"))
	_, _ = db.Exec(`DELETE FROM backups WHERE id=?`, bid)
	audit(r, u.Username, "backup.delete", in.Name+": "+bid)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

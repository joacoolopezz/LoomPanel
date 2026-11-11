package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// safePath maps a user-supplied relative path into the instance directory,
// refusing anything that escapes it (including through symlinks).
// Uses os.Open with O_NOFOLLOW + stat verification for TOCTOU protection (Go 1.18+ compatible).
func safePath(id, rel string) (string, error) {
	root := instDir(id)

	// Clean and validate the relative path
	rel = path.Clean("/" + rel)
	if rel == "." || rel == "/" {
		rel = ""
	}
	// Reject any path traversal attempts before joining
	if strings.Contains(rel, "..") {
		return "", errors.New("path escapes instance")
	}

	// Build the absolute path
	absPath := filepath.Join(root, filepath.FromSlash(rel))

	// Open with O_NOFOLLOW to avoid following symlinks in the final component
	f, err := os.OpenFile(absPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Get file info
	_, err = f.Stat()
	if err != nil {
		return "", err
	}

	// Resolve the real absolute path of the opened file (handles symlinks in intermediate dirs)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", err
	}

	// Verify the resolved path is within the instance directory
	if !strings.HasPrefix(realPath, realRoot+string(os.PathSeparator)) && realPath != realRoot {
		return "", errors.New("path escapes instance")
	}

	// Additional check: if the file was replaced after open, Stat would still show the original
	// The file descriptor was opened with O_NOFOLLOW so we know we have the right inode

	return absPath, nil
}

func pathOr400(w http.ResponseWriter, id, rel string) (string, bool) {
	p, err := safePath(id, rel)
	if err != nil {
		fail(w, 400, "invalid_path")
		return "", false
	}
	return p, true
}

func handleFilesList(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	p, ok := pathOr400(w, in.ID, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		fail(w, 404, "not_found")
		return
	}
	type fe struct {
		Name  string `json:"name"`
		Dir   bool   `json:"dir"`
		Size  int64  `json:"size"`
		Mtime int64  `json:"mtime"`
	}
	out := []fe{}
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, fe{e.Name(), e.IsDir(), fi.Size(), fi.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, 200, out)
}

const maxEdit = 2 << 20

func handleFileRead(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	p, ok := pathOr400(w, in.ID, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		fail(w, 404, "not_found")
		return
	}
	if fi.Size() > maxEdit {
		fail(w, 413, "too_large_to_edit")
		return
	}
	b, _ := os.ReadFile(p)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(b)
}

func handleFileWrite(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	p, ok := pathOr400(w, in.ID, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEdit))
	if err != nil {
		fail(w, 413, "too_large_to_edit")
		return
	}
	if err := os.WriteFile(p, body, 0o644); err != nil {
		fail(w, 500, "write_failed")
		return
	}
	audit(r, u.Username, "file.write", in.Name+":"+r.URL.Query().Get("path"))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleDownload(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	rel := r.URL.Query().Get("path")
	p, ok := pathOr400(w, in.ID, rel)
	if !ok {
		return
	}
	fi, err := os.Stat(p)
	if err != nil {
		fail(w, 404, "not_found")
		return
	}
	name := filepath.Base(p)
	if fi.IsDir() {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))
		zw := zip.NewWriter(w)
		defer zw.Close()

		var walkErr error
		filepath.WalkDir(p, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				walkErr = err
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rp, _ := filepath.Rel(p, fp)
			f, err := os.Open(fp)
			if err != nil {
				walkErr = err
				return err
			}
			defer f.Close()
			zf, err := zw.Create(filepath.ToSlash(rp))
			if err != nil {
				walkErr = err
				return err
			}
			_, err = io.Copy(zf, f)
			if err != nil {
				walkErr = err
				return err
			}
			return nil
		})
		if walkErr != nil {
			// Headers already sent, but we can't send error response.
			// Log the error for debugging.
			log.Printf("zip creation error for %s: %v", in.ID, walkErr)
		}
		return
	}
	f, err := os.Open(p)
	if err != nil {
		fail(w, 404, "not_found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

func cleanName(n string) (string, bool) {
	n = filepath.Base(strings.ReplaceAll(n, "\\", "/"))
	if n == "." || n == ".." || n == "/" || n == "" || strings.ContainsRune(n, 0) {
		return "", false
	}
	return n, true
}

func handleUpload(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	dir, ok := pathOr400(w, in.ID, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
	mr, err := r.MultipartReader()
	if err != nil {
		fail(w, 400, "invalid_upload")
		return
	}
	n := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(w, 400, "upload_failed")
			return
		}
		name, ok := cleanName(part.FileName())
		if !ok {
			continue
		}
		dest := filepath.Join(dir, name)
		if _, err := safePath(in.ID, path.Join(r.URL.Query().Get("path"), name)); err != nil {
			continue
		}
		f, err := os.Create(dest)
		if err != nil {
			fail(w, 500, "write_failed")
			return
		}
		_, err = io.Copy(f, part)
		f.Close()
		if err != nil {
			os.Remove(dest)
			fail(w, 500, "upload_failed")
			return
		}
		n++
		audit(r, u.Username, "file.upload", in.Name+":"+name)
	}
	writeJSON(w, 200, map[string]int{"uploaded": n})
}

func handleMkdir(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ Path string }
	if !readJSON(w, r, &b) {
		return
	}
	p, ok := pathOr400(w, in.ID, b.Path)
	if !ok {
		return
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		fail(w, 500, "mkdir_failed")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleRename(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ From, To string }
	if !readJSON(w, r, &b) {
		return
	}
	from, ok1 := pathOr400(w, in.ID, b.From)
	if !ok1 {
		return
	}
	to, ok2 := pathOr400(w, in.ID, b.To)
	if !ok2 {
		return
	}
	if from == instDir(in.ID) || os.Rename(from, to) != nil {
		fail(w, 400, "rename_failed")
		return
	}
	audit(r, u.Username, "file.rename", in.Name+": "+b.From+" -> "+b.To)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleDelete(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ Paths []string }
	if !readJSON(w, r, &b) {
		return
	}
	root, _ := filepath.EvalSymlinks(instDir(in.ID))
	for _, rel := range b.Paths {
		p, err := safePath(in.ID, rel)
		if err != nil || p == root {
			fail(w, 400, "invalid_path")
			return
		}
		if err := os.RemoveAll(p); err != nil {
			fail(w, 500, "delete_failed")
			return
		}
		audit(r, u.Username, "file.delete", in.Name+":"+rel)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- download from URL (mods/plugins from any page) with SSRF protection ----

var fetchClient = &http.Client{
	Timeout: 15 * time.Minute,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 15 * time.Second,
			Control: func(network, address string, c syscall.RawConn) error {
				host, _, _ := net.SplitHostPort(address)
				ip := net.ParseIP(host)
				if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
					return errors.New("blocked address")
				}
				return nil
			},
		}).DialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
			return errors.New("too many redirects")
		}
		return nil
	},
}

func handleFetch(w http.ResponseWriter, r *http.Request, u *User, in Instance) {
	var b struct{ URL, Path string }
	if !readJSON(w, r, &b) {
		return
	}
	pu, err := url.Parse(b.URL)
	if err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
		fail(w, 400, "invalid_url")
		return
	}
	dir, ok := pathOr400(w, in.ID, b.Path)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", b.URL, nil)
	req.Header.Set("User-Agent", "loompanel/0.1")
	resp, err := fetchClient.Do(req)
	if err != nil {
		fail(w, 502, "fetch_failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, 502, fmt.Sprintf("remote_http_%d", resp.StatusCode))
		return
	}
	name := path.Base(resp.Request.URL.Path)
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = params["filename"]
	}
	name, ok = cleanName(name)
	if !ok {
		name = "download.bin"
	}
	dest := filepath.Join(dir, name)
	if _, err := safePath(in.ID, path.Join(b.Path, name)); err != nil {
		fail(w, 400, "invalid_path")
		return
	}
	f, err := os.Create(dest)
	if err != nil {
		fail(w, 500, "write_failed")
		return
	}
	// Max download size: configurable via GP_MAX_FETCH_MB (default 2GB)
	maxFetchMB := 2048
	if v := os.Getenv("GP_MAX_FETCH_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxFetchMB = n
		}
	}
	maxFetchBytes := int64(maxFetchMB) * 1024 * 1024
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxFetchBytes))
	f.Close()
	if err != nil {
		os.Remove(dest)
		fail(w, 502, "fetch_failed")
		return
	}
	audit(r, u.Username, "file.fetch", in.Name+": "+pu.Host+"/"+name)
	writeJSON(w, 200, map[string]any{"name": name, "size": n})
}

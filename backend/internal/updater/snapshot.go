package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	_ "modernc.org/sqlite"
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

const snapshotLimit = 256 << 20

var snapshotMagic = []byte("DEVBOX-UPDATE-STATE-v1\n")

type snapshotRoot struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
}

func checkUpdateIdle(filename string) error {
	if !filepath.IsAbs(filename) {
		return fmt.Errorf("database path must be absolute")
	}
	u := url.URL{Scheme: "file", Path: filename, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var count int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status IN ('running','queued')`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("update refused: %d jobs remain queued/running", count)
	}
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("database integrity check failed")
	}
	return nil
}
func safeSnapshotRoots(paths []string) ([]snapshotRoot, error) {
	if len(paths) == 0 || len(paths) > 32 {
		return nil, fmt.Errorf("snapshot needs 1..32 explicit paths")
	}
	roots := make([]snapshotRoot, 0, len(paths))
	for _, p := range paths {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" || len(strings.Split(strings.Trim(p, "/"), "/")) < 2 {
			return nil, fmt.Errorf("unsafe snapshot root")
		}
		for _, r := range roots {
			if p == r.Path || strings.HasPrefix(p, r.Path+"/") || strings.HasPrefix(r.Path, p+"/") {
				return nil, fmt.Errorf("snapshot roots overlap")
			}
		}
		_, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		roots = append(roots, snapshotRoot{p, err == nil})
	}
	return roots, nil
}
func allowedSnapshotLink(target, link string, roots []snapshotRoot) bool {
	resolved := link
	if !filepath.IsAbs(link) {
		resolved = filepath.Join(filepath.Dir(target), link)
	}
	resolved = filepath.Clean(resolved)
	for _, r := range roots {
		if resolved == r.Path || strings.HasPrefix(resolved, r.Path+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
func createUpdateSnapshot(filename string, paths []string, key []byte) error {
	roots, err := safeSnapshotRoots(paths)
	if err != nil {
		return err
	}
	var plain bytes.Buffer
	gz := gzip.NewWriter(&plain)
	tw := tar.NewWriter(gz)
	metadata, _ := json.Marshal(roots)
	if err = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(metadata)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err = tw.Write(metadata); err != nil {
		return err
	}
	var total int64
	count := 0
	for i, r := range roots {
		if !r.Existed {
			continue
		}
		err = filepath.WalkDir(r.Path, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			count++
			if count > 100000 {
				return fmt.Errorf("snapshot contains too many files")
			}
			link := ""
			if fi.Mode()&os.ModeSymlink != 0 {
				link, err = os.Readlink(p)
				if err != nil {
					return err
				}
				if !allowedSnapshotLink(p, link, roots) {
					return fmt.Errorf("snapshot refuses symlink outside selected roots")
				}
			}
			if !fi.IsDir() && !fi.Mode().IsRegular() && fi.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("snapshot refuses special file")
			}
			h, err := tar.FileInfoHeader(fi, link)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(r.Path, p)
			if err != nil {
				return err
			}
			h.Name = "roots/" + strconv.Itoa(i)
			if rel != "." {
				h.Name += "/" + filepath.ToSlash(rel)
			}
			total += h.Size
			if total > snapshotLimit {
				return fmt.Errorf("control-plane snapshot exceeds 256 MiB; no changes applied")
			}
			if err = tw.WriteHeader(h); err != nil {
				return err
			}
			if !fi.Mode().IsRegular() {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.CopyN(tw, f, fi.Size())
			ce := f.Close()
			if err != nil {
				return err
			}
			return ce
		})
		if err != nil {
			return err
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	encrypted := append(append([]byte{}, snapshotMagic...), nonce...)
	encrypted = aead.Seal(encrypted, nonce, plain.Bytes(), snapshotMagic)
	clear(plain.Bytes())
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(filename)
		}
	}()
	if _, err = f.Write(encrypted); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}
func ensureRealParents(p string) error {
	parent := filepath.Dir(p)
	for current := parent; current != "/"; current = filepath.Dir(current) {
		fi, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("restore parent is not a real directory")
		}
	}
	return os.MkdirAll(parent, 0755)
}
func restoreUpdateSnapshot(filename string, key []byte) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, snapshotLimit+(2<<20)))
	f.Close()
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	offset := len(snapshotMagic) + aead.NonceSize()
	if len(data) < offset || !bytes.HasPrefix(data, snapshotMagic) {
		return fmt.Errorf("invalid encrypted update snapshot")
	}
	plain, err := aead.Open(nil, data[len(snapshotMagic):offset], data[offset:], snapshotMagic)
	if err != nil {
		return fmt.Errorf("snapshot authentication failed")
	}
	defer clear(plain)
	gz, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	h, err := tr.Next()
	if err != nil || h.Name != "manifest.json" || h.Size > 65536 {
		return fmt.Errorf("invalid snapshot manifest")
	}
	meta, err := io.ReadAll(tr)
	if err != nil {
		return err
	}
	var roots []snapshotRoot
	if err = json.Unmarshal(meta, &roots); err != nil {
		return err
	}
	paths := make([]string, 0, len(roots))
	for _, r := range roots {
		paths = append(paths, r.Path)
	}
	if _, err = safeSnapshotRoots(paths); err != nil {
		return err
	}
	// Prepare all roots before touching the installation. Each stage is on the
	// target filesystem, so activation uses rename rather than cross-device copy.
	stages := make([]string, len(roots))
	keep := false
	defer func() {
		if !keep {
			for _, p := range stages {
				if p != "" {
					os.RemoveAll(p)
				}
			}
		}
	}()
	for i, r := range roots {
		if err = ensureRealParents(r.Path); err != nil {
			return err
		}
		stages[i], err = os.MkdirTemp(filepath.Dir(r.Path), ".devbox-restore-")
		if err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	type attributes struct {
		path   string
		header tar.Header
	}
	attrs := []attributes{}
	var total int64
	for count := 0; ; count++ {
		h, err = tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		parts := strings.Split(h.Name, "/")
		if count > 100000 || len(parts) < 2 || parts[0] != "roots" || path.Clean(h.Name) != h.Name || seen[h.Name] || strings.Contains(h.Name, "\\") {
			return fmt.Errorf("invalid snapshot entry")
		}
		seen[h.Name] = true
		i, err := strconv.Atoi(parts[1])
		if err != nil || i < 0 || i >= len(roots) || !roots[i].Existed {
			return fmt.Errorf("snapshot references invalid root")
		}
		rel := strings.Join(parts[2:], "/")
		target := filepath.Join(stages[i], "root", filepath.FromSlash(rel))
		base := filepath.Join(stages[i], "root")
		if target != base && !strings.HasPrefix(target, base+"/") {
			return fmt.Errorf("snapshot path escapes root")
		}
		if err = ensureRealParents(target); err != nil {
			return err
		}
		if h.Size < 0 || h.Size > snapshotLimit-total {
			return fmt.Errorf("snapshot size limit exceeded")
		}
		total += h.Size
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0700)
		case tar.TypeSymlink:
			if !allowedSnapshotLink(filepath.Join(roots[i].Path, rel), h.Linkname, roots) {
				return fmt.Errorf("invalid snapshot link")
			}
			err = os.Symlink(h.Linkname, target)
		case tar.TypeReg:
			file, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return e
			}
			_, err = io.CopyN(file, tr, h.Size)
			ce := file.Close()
			if err == nil {
				err = ce
			}
		default:
			return fmt.Errorf("unsupported snapshot file type")
		}
		if err != nil {
			return err
		}
		attrs = append(attrs, attributes{target, *h})
	}
	if _, err = io.Copy(io.Discard, gz); err != nil {
		return err
	}
	sort.SliceStable(attrs, func(i, j int) bool { return len(attrs[i].path) > len(attrs[j].path) })
	for _, a := range attrs {
		if os.Geteuid() == 0 {
			if err = os.Lchown(a.path, a.header.Uid, a.header.Gid); err != nil {
				return err
			}
		}
		if a.header.Typeflag != tar.TypeSymlink {
			if err = os.Chmod(a.path, os.FileMode(a.header.Mode)&0777); err != nil {
				return err
			}
		}
	}
	for i, r := range roots {
		if r.Existed {
			if _, err = os.Lstat(filepath.Join(stages[i], "root")); err != nil {
				return fmt.Errorf("snapshot root is missing")
			}
		}
	}
	type activation struct {
		i         int
		had       bool
		installed bool
	}
	done := []activation{}
	rollback := func() error {
		var result error
		for j := len(done) - 1; j >= 0; j-- {
			a := done[j]
			if a.installed {
				result = errors.Join(result, os.RemoveAll(roots[a.i].Path))
			}
			if a.had {
				result = errors.Join(result, os.Rename(filepath.Join(stages[a.i], "previous"), roots[a.i].Path))
			}
		}
		if result != nil {
			keep = true
			return fmt.Errorf("restore rollback failed; retained .devbox-restore directories: %w", result)
		}
		return nil
	}
	for i, r := range roots {
		a := activation{i: i}
		if _, err = os.Lstat(r.Path); err == nil {
			if err = os.Rename(r.Path, filepath.Join(stages[i], "previous")); err != nil {
				return errors.Join(err, rollback())
			}
			a.had = true
		} else if !os.IsNotExist(err) {
			return errors.Join(err, rollback())
		}
		done = append(done, a)
		if r.Existed {
			if err = os.Rename(filepath.Join(stages[i], "root"), r.Path); err != nil {
				return errors.Join(err, rollback())
			}
			done[len(done)-1].installed = true
		}
	}
	for _, r := range roots {
		if d, err := os.Open(filepath.Dir(r.Path)); err == nil {
			_ = syscall.Fsync(int(d.Fd()))
			_ = d.Close()
		}
	}
	return nil
}

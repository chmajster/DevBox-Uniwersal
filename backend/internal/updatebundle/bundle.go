// Package updatebundle verifies signed source releases before any installer runs.
package updatebundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const MaxArchive = 64 << 20
const MaxExpanded = 256 << 20

type Manifest struct {
	Schema    int    `json:"schema"`
	Version   string `json:"version"`
	Tag       string `json:"tag"`
	SHA256    string `json:"sha256"`
	CreatedAt string `json:"created_at"`
}

var revision = regexp.MustCompile(`^[0-9a-f]{40}$`)
var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)

func ReadLimited(filename string, maximum int64) ([]byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maximum {
		return nil, fmt.Errorf("file exceeds permitted size")
	}
	return b, nil
}
func PublicKey(filename string) (ed25519.PublicKey, error) {
	raw, err := ReadLimited(filename, 4096)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("trusted update key must contain one base64 Ed25519 public key")
	}
	return ed25519.PublicKey(key), nil
}
func Sign(archive []byte, version, tag string, key ed25519.PrivateKey) ([]byte, []byte, error) {
	if !revision.MatchString(version) || !releaseTag.MatchString(tag) || len(key) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("invalid release version, tag or signing key")
	}
	hash := sha256.Sum256(archive)
	data, err := json.Marshal(Manifest{1, version, tag, hex.EncodeToString(hash[:]), time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return nil, nil, err
	}
	return data, ed25519.Sign(key, data), nil
}
func VerifyManifest(manifest, signature []byte, key ed25519.PublicKey) (Manifest, error) {
	var m Manifest
	if len(manifest) > 16<<10 || len(signature) != ed25519.SignatureSize || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, manifest, signature) {
		return m, fmt.Errorf("release signature verification failed")
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return m, fmt.Errorf("invalid signed manifest")
	}
	digest, err := hex.DecodeString(m.SHA256)
	if m.Schema != 1 || !revision.MatchString(m.Version) || !releaseTag.MatchString(m.Tag) || err != nil || len(digest) != sha256.Size {
		return m, fmt.Errorf("invalid release manifest")
	}
	created, err := time.Parse(time.RFC3339, m.CreatedAt)
	if err != nil || created.After(time.Now().Add(24*time.Hour)) {
		return m, fmt.Errorf("invalid release date")
	}
	return m, nil
}
func Verify(manifest, signature, archive []byte, key ed25519.PublicKey) (Manifest, error) {
	m, err := VerifyManifest(manifest, signature, key)
	if err != nil {
		return m, err
	}
	digest := sha256.Sum256(archive)
	if len(archive) > MaxArchive || m.SHA256 != hex.EncodeToString(digest[:]) {
		return m, fmt.Errorf("release archive checksum mismatch")
	}
	return m, nil
}

// Extract permits regular files/directories only. No links, devices, traversal,
// .git metadata or path aliases are accepted, including entries later in a tar.
func Extract(reader io.Reader, destination string) error {
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return fmt.Errorf("release destination must not exist")
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(destination)
		}
	}()
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tr := tar.NewReader(gzipReader)
	seen := map[string]bool{}
	var size int64
	for count := 0; ; count++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			if h.Size > 16384 {
				return fmt.Errorf("oversized archive metadata")
			}
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || name == "." {
			if h.Typeflag == tar.TypeDir {
				continue
			}
			return fmt.Errorf("invalid archive entry")
		}
		if count > 100000 || path.Clean(name) != name || path.IsAbs(name) || strings.Contains(name, "\\") || strings.HasPrefix(name, "../") || name == ".." || seen[name] {
			return fmt.Errorf("unsafe archive entry")
		}
		for _, p := range strings.Split(name, "/") {
			if p == ".git" {
				return fmt.Errorf("VCS metadata is forbidden in release archives")
			}
		}
		seen[name] = true
		target := filepath.Join(destination, filepath.FromSlash(name))
		if h.Size < 0 || h.Size > MaxExpanded-size {
			return fmt.Errorf("expanded release exceeds size limit")
		}
		size += h.Size
		if h.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("only regular files and directories are supported in releases")
		}
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if h.Mode&0111 != 0 {
			mode = 0755
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// Consume the trailer as well: do not accept a gzip stream with a bad CRC.
	if n, tailErr := io.Copy(io.Discard, io.LimitReader(gzipReader, 1<<20)); tailErr != nil || n == 1<<20 {
		return fmt.Errorf("invalid or oversized archive trailer")
	}
	if err = gzipReader.Close(); err != nil {
		return err
	}
	for _, name := range []string{"install.sh", "backend/go.mod", "frontend/package-lock.json"} {
		fi, err := os.Stat(filepath.Join(destination, name))
		if err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("release is missing required source files")
		}
	}
	ok = true
	return nil
}

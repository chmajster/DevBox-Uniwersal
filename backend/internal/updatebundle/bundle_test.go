package updatebundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveFixture(t *testing.T, malicious *tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"install.sh", "backend/go.mod", "frontend/package-lock.json"} {
		data := []byte("release-content")
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if malicious != nil {
		if err := tw.WriteHeader(malicious); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func TestSignedReleaseAuthenticatesManifestAndArchive(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	archive := archiveFixture(t, nil)
	m, sig, err := Sign(archive, strings.Repeat("a", 40), "v1.2.3", key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(m, sig, archive, pub); err != nil {
		t.Fatal(err)
	}
	mutated := append([]byte{}, archive...)
	mutated[10] ^= 1
	if _, err = Verify(m, sig, mutated, pub); err == nil {
		t.Fatal("accepted tampered archive")
	}
	sig[0] ^= 1
	if _, err = Verify(m, sig, archive, pub); err == nil {
		t.Fatal("accepted tampered signature")
	}
	sig[0] ^= 1
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = Verify(m, sig, archive, other); err == nil {
		t.Fatal("accepted untrusted signing key")
	}
	m[0] ^= 1
	if _, err = Verify(m, sig, archive, pub); err == nil {
		t.Fatal("accepted changed manifest")
	}
}
func TestExtractIsBoundedAndNeverFollowsLinks(t *testing.T) {
	good := archiveFixture(t, nil)
	dest := filepath.Join(t.TempDir(), "release")
	if err := Extract(bytes.NewReader(good), dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "backend/go.mod")); err != nil {
		t.Fatal(err)
	}
	if err := Extract(bytes.NewReader(good), dest); err == nil {
		t.Fatal("overwrote existing directory")
	}
	for _, h := range []tar.Header{
		{Name: "../escape", Typeflag: tar.TypeReg}, {Name: "/absolute", Typeflag: tar.TypeReg},
		{Name: ".git/config", Typeflag: tar.TypeReg}, {Name: "install.sh", Typeflag: tar.TypeReg},
		{Name: "unsafe", Typeflag: tar.TypeSymlink, Linkname: "/etc"}, {Name: "hard", Typeflag: tar.TypeLink, Linkname: "install.sh"},
		{Name: "device", Typeflag: tar.TypeChar},
	} {
		t.Run(h.Name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "release")
			if err := Extract(bytes.NewReader(archiveFixture(t, &h)), dest); err == nil {
				t.Fatal("accepted unsafe archive")
			}
			if _, err := os.Lstat(dest); !os.IsNotExist(err) {
				t.Fatal("failed extraction left a partial installation")
			}
		})
	}
	corrupt := append([]byte{}, good...)
	corrupt[len(corrupt)-5] ^= 1
	if err := Extract(bytes.NewReader(corrupt), filepath.Join(t.TempDir(), "release")); err == nil {
		t.Fatal("accepted corrupt gzip checksum")
	}
}

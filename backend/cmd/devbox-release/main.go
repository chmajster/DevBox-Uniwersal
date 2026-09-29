package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/updatebundle"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	generate := flag.Bool("generate-key", false, "generate a signing key pair (keep private key outside the repository)")
	out := flag.String("output-dir", ".", "output directory")
	archive := flag.String("archive", "", "source tar.gz")
	version := flag.String("version", "", "full commit SHA")
	tag := flag.String("tag", "", "release tag vX.Y.Z")
	keyFile := flag.String("key-file", "", "base64 Ed25519 private key file")
	flag.Parse()
	if err := os.MkdirAll(*out, 0700); err != nil {
		return err
	}
	write := func(name string, data []byte, mode os.FileMode) error {
		f, err := os.OpenFile(filepath.Join(*out, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		ce := f.Close()
		if err != nil {
			return err
		}
		return ce
	}
	if *generate {
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		defer clear(private)
		if err = write("update-signing.key", []byte(base64.StdEncoding.EncodeToString(private)+"\n"), 0600); err != nil {
			return err
		}
		return write("update-public.key", []byte(base64.StdEncoding.EncodeToString(public)+"\n"), 0644)
	}
	raw, err := updatebundle.ReadLimited(*keyFile, 4096)
	if err != nil {
		return err
	}
	defer clear(raw)
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid signing key")
	}
	defer clear(key)
	bundle, err := updatebundle.ReadLimited(*archive, updatebundle.MaxArchive)
	if err != nil {
		return err
	}
	manifest, signature, err := updatebundle.Sign(bundle, *version, *tag, ed25519.PrivateKey(key))
	if err != nil {
		return err
	}
	if err = write("devbox-manifest.json", manifest, 0644); err != nil {
		return err
	}
	return write("devbox-manifest.sig", []byte(base64.StdEncoding.EncodeToString(signature)+"\n"), 0644)
}

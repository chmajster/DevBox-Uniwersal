package proxy

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalCertificatesUseStableEncryptedCAAndRestrictedFiles(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(filepath.Join(t.TempDir(), "tls.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(ctx, db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewAESGCMFromBase64(key)
	if err != nil {
		t.Fatal(err)
	}
	store := secrets.NewSQLiteStore(db, cipher)
	nginx := NewNginxProvider(NginxOptions{})
	network := &Service{repo: NewSQLiteRepository(db), nginx: nginx}
	runner := jobs.NewRunner(repository.NewSQLiteJobs(db))
	service, err := NewTLSService(db, store, network, runner, nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.issue(ctx, Domain{ID: "test-domain", Hostname: "audit.test"})
	if err != nil {
		t.Fatal(err)
	}
	ca, _, err := service.authority(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(first.PEM))
	if block == nil {
		t.Fatal("not PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if leaf.VerifyHostname("audit.test") != nil || leaf.CheckSignatureFrom(ca) != nil || leaf.VerifyHostname("another.test") == nil {
		t.Fatal("invalid certificate identity/chain")
	}
	certFile, keyFile, err := service.writeMaterial(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{certFile, keyFile} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe material: %s %v", file, err)
		}
	}
	renewed, err := service.issue(ctx, Domain{ID: "test-domain", Hostname: "audit.test"})
	if err != nil {
		t.Fatal(err)
	}
	caAgain, _, err := service.authority(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == renewed.Fingerprint || fingerprint(ca.Raw) != fingerprint(caAgain.Raw) {
		t.Fatal("renewal did not preserve CA or replace leaf")
	}
	wrong := first
	wrong.Hostname = "wrong.test"
	if _, _, err = service.writeMaterial(ctx, wrong); err == nil {
		t.Fatal("accepted mismatching certificate hostname")
	}
	if _, err = service.issue(ctx, Domain{ID: "public", Hostname: "example.com"}); err == nil {
		t.Fatal("local CA issued public domain unexpectedly")
	}
	var ciphertext []byte
	if err = db.QueryRow(`SELECT ciphertext FROM secrets WHERE scope=? AND name=?`, tlsScope, first.KeyName).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ciphertext), "PRIVATE KEY") {
		t.Fatal("stored plaintext private key")
	}
	route := providers.ProxyRoute{Domain: "audit.test", Upstream: "http://127.0.0.1:8080", TLS: true, Metadata: map[string]string{"tls_certificate": certFile, "tls_key": keyFile}}
	rendered, err := nginx.Render(route)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"listen 443 ssl", "ssl_protocols TLSv1.2 TLSv1.3", "return 308 https://audit.test$request_uri", certFile, keyFile} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("TLS config missing %s", expected)
		}
	}
	route.Metadata["tls_key"] = "/tmp/key;return 200;"
	if _, err = nginx.Render(route); err == nil {
		t.Fatal("accepted nginx directive injection")
	}
}

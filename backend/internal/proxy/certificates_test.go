package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func TestExistingCertificateTerminatesTLSAtValidatedDomainProxy(t *testing.T) {
	root := t.TempDir()
	host := "app.local"
	directory := filepath.Join(root, host)
	os.Mkdir(directory, 0700)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(directory, "fullchain.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	os.WriteFile(filepath.Join(directory, "privkey.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600)
	provider := NewNginxProvider(NginxOptions{CertificateDir: root})
	runner := &fakeNginxRunner{}
	provider.runner = runner
	route := providers.ProxyRoute{Domain: host, Upstream: "http://127.0.0.1:32781", TLS: true}
	config, err := provider.Render(route)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"listen 443 ssl", "ssl_certificate", "proxy_pass http://127.0.0.1:32781"} {
		if !strings.Contains(config, part) {
			t.Fatal("missing TLS proxy directive", part)
		}
	}
	if err := provider.TestRoute(context.Background(), route); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.candidateConfig, " ssl;") {
		t.Fatal("candidate TLS omitted")
	}
	if _, _, err := provider.certificatePaths("other.local"); err == nil {
		t.Fatal("missing certificate accepted")
	}
	certificate.NotAfter = time.Now().Add(-time.Minute)
	der, err = x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(directory, "fullchain.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	if _, _, err := provider.certificatePaths(host); err == nil {
		t.Fatal("expired certificate accepted")
	}
}

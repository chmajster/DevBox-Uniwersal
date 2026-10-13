package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Certificates belong to domains. Runtime containers retain an HTTP listener.
// The certificate directory is operator configuration, not a request parameter.
func (n *NginxProvider) certificatePaths(domain string) (string, string, error) {
	if n.options.CertificateDir == "" {
		return "", "", fmt.Errorf("existing TLS certificates are not configured")
	}
	host, err := NormalizeHostname(domain)
	if err != nil {
		return "", "", err
	}
	root, err := filepath.Abs(n.options.CertificateDir)
	if err != nil {
		return "", "", err
	}
	if strings.ContainsAny(root, "\x00\r\n\";{}$") {
		return "", "", fmt.Errorf("invalid certificate directory")
	}
	cert := filepath.Join(root, host, "fullchain.pem")
	key := filepath.Join(root, host, "privkey.pem")
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return "", "", fmt.Errorf("load domain certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", "", err
	}
	if err := leaf.VerifyHostname(host); err != nil {
		return "", "", err
	}
	if now := time.Now(); now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return "", "", fmt.Errorf("domain certificate is not currently valid")
	}
	return cert, key, nil
}

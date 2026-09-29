package proxy

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

const tlsScope = "proxy/tls"

type authorityBundle struct {
	Certificate []byte `json:"certificate"`
	Key         []byte `json:"key"`
}
type certificateRecord struct{ DomainID, Hostname, PEM, KeyName, Fingerprint, NotAfter, VerifiedAt string }
type TLSService struct {
	db          *sql.DB
	secrets     secrets.SecretStore
	network     *Service
	runner      jobs.JobRunner
	audit       *audit.Service
	directory   string
	authorityMu sync.Mutex
	verify      func(context.Context, string, string, *x509.Certificate) error
}

func NewTLSService(db *sql.DB, store secrets.SecretStore, network *Service, runner jobs.JobRunner, auditor *audit.Service, directory string) (*TLSService, error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("TLS material directory must be a real directory")
	}
	if err = os.Chmod(directory, 0700); err != nil {
		return nil, err
	}
	s := &TLSService{db: db, secrets: store, network: network, runner: runner, audit: auditor, directory: directory, verify: verifyTLSListener}
	network.nginx.certificates = s
	if err = runner.Register(s); err != nil {
		return nil, err
	}
	return s, nil
}
func serialNumber() (*big.Int, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	return n.Add(n, big.NewInt(1)), nil
}
func fingerprint(der []byte) string { sum := sha256.Sum256(der); return hex.EncodeToString(sum[:]) }
func localCertificateHostname(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate()
	}
	for _, suffix := range []string{".localhost", ".test", ".local", ".internal", ".lan"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}
func (s *TLSService) authority(ctx context.Context, create bool) (*x509.Certificate, crypto.Signer, error) {
	s.authorityMu.Lock()
	defer s.authorityMu.Unlock()
	if s.secrets == nil {
		return nil, nil, fmt.Errorf("SecretStore is required for TLS")
	}
	raw, err := s.secrets.Get(ctx, tlsScope, "authority")
	if errors.Is(err, secrets.ErrNotFound) && create {
		var count int
		if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domain_certificates`).Scan(&count); e != nil {
			return nil, nil, e
		}
		if count > 0 {
			return nil, nil, fmt.Errorf("CA key is missing while certificates exist; restore the correct SecretStore instead of silently replacing trust")
		}
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return nil, nil, e
		}
		serial, e := serialNumber()
		if e != nil {
			return nil, nil, e
		}
		now := time.Now().UTC()
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "DevBox Universal Local Development CA"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
		der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if e != nil {
			return nil, nil, e
		}
		private, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return nil, nil, e
		}
		raw, e = json.Marshal(authorityBundle{Certificate: der, Key: private})
		clear(private)
		if e != nil {
			return nil, nil, e
		}
		if e = s.secrets.Put(ctx, tlsScope, "authority", raw); e != nil {
			clear(raw)
			return nil, nil, e
		}
		err = nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("local CA is not initialized; enable HTTPS on a local domain first: %w", err)
	}
	defer clear(raw)
	var bundle authorityBundle
	if err = json.Unmarshal(raw, &bundle); err != nil {
		return nil, nil, fmt.Errorf("invalid CA state")
	}
	defer clear(bundle.Key)
	certificate, err := x509.ParseCertificate(bundle.Certificate)
	if err != nil {
		return nil, nil, err
	}
	private, err := x509.ParsePKCS8PrivateKey(bundle.Key)
	if err != nil {
		return nil, nil, err
	}
	signer, ok := private.(crypto.Signer)
	if !ok {
		return nil, nil, fmt.Errorf("invalid CA signing key")
	}
	actual, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return nil, nil, err
	}
	expected, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	if !certificate.IsCA || !bytes.Equal(actual, expected) || time.Until(certificate.NotAfter) < 24*time.Hour {
		return nil, nil, fmt.Errorf("CA key mismatch or CA expires within 24 hours; trust must be renewed explicitly")
	}
	return certificate, signer, nil
}
func (s *TLSService) issue(ctx context.Context, domain Domain) (certificateRecord, error) {
	if !localCertificateHostname(domain.Hostname) {
		return certificateRecord{}, fmt.Errorf("local CA is limited to localhost, private IPs and .localhost/.test/.local/.internal/.lan domains; public domains require a publicly trusted certificate")
	}
	ca, key, err := s.authority(ctx, true)
	if err != nil {
		return certificateRecord{}, err
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return certificateRecord{}, err
	}
	serial, err := serialNumber()
	if err != nil {
		return certificateRecord{}, err
	}
	now := time.Now().UTC()
	expires := now.AddDate(0, 0, 90)
	if expires.After(ca.NotAfter) {
		expires = ca.NotAfter.Add(-time.Minute)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: domain.Hostname}, NotBefore: now.Add(-5 * time.Minute), NotAfter: expires, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	if ip := net.ParseIP(domain.Hostname); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{domain.Hostname}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &private.PublicKey, key)
	if err != nil {
		return certificateRecord{}, err
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return certificateRecord{}, err
	}
	defer clear(encoded)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	defer clear(keyPEM)
	record := certificateRecord{DomainID: domain.ID, Hostname: domain.Hostname, PEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), KeyName: "leaf-" + domain.ID + "-" + serial.Text(16), Fingerprint: fingerprint(der), NotAfter: expires.Format(time.RFC3339Nano)}
	if err = s.secrets.Put(ctx, tlsScope, record.KeyName, keyPEM); err != nil {
		return certificateRecord{}, err
	}
	return record, nil
}
func (s *TLSService) record(ctx context.Context, id string) (certificateRecord, error) {
	var r certificateRecord
	r.DomainID = id
	err := s.db.QueryRowContext(ctx, `SELECT hostname,certificate_pem,key_secret_name,fingerprint,not_after,verified_at FROM domain_certificates WHERE domain_id=?`, id).Scan(&r.Hostname, &r.PEM, &r.KeyName, &r.Fingerprint, &r.NotAfter, &r.VerifiedAt)
	return r, err
}
func (s *TLSService) writeMaterial(ctx context.Context, r certificateRecord) (string, string, error) {
	key, err := s.secrets.Get(ctx, tlsScope, r.KeyName)
	if err != nil {
		return "", "", err
	}
	defer clear(key)
	pair, err := tls.X509KeyPair([]byte(r.PEM), key)
	if err != nil {
		return "", "", fmt.Errorf("TLS certificate/private key mismatch")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return "", "", err
	}
	if fingerprint(leaf.Raw) != r.Fingerprint || leaf.VerifyHostname(r.Hostname) != nil {
		return "", "", fmt.Errorf("TLS certificate identity mismatch")
	}
	ca, _, err := s.authority(ctx, false)
	if err != nil {
		return "", "", err
	}
	if err = leaf.CheckSignatureFrom(ca); err != nil {
		return "", "", err
	}
	certPath := filepath.Join(s.directory, r.Fingerprint+".pem")
	keyPath := filepath.Join(s.directory, r.Fingerprint+".key")
	if err = atomicWriteFile(keyPath, key, 0600); err != nil {
		return "", "", err
	}
	if err = atomicWriteFile(certPath, []byte(r.PEM), 0600); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}
func (s *TLSService) Material(ctx context.Context, hostname string) (string, string, error) {
	d, err := s.network.repo.DomainByHostname(ctx, hostname)
	if err != nil {
		return "", "", err
	}
	r, err := s.record(ctx, d.ID)
	if err != nil {
		return "", "", err
	}
	return s.writeMaterial(ctx, r)
}

// Rehydrate private files from encrypted state, including after control-plane restore.
func (s *TLSService) RestoreMaterial(ctx context.Context) error {
	domains, err := s.network.repo.ListDomains(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, d := range domains {
		if !d.TLSEnabled {
			continue
		}
		if _, _, err = s.Material(ctx, d.Hostname); err != nil {
			failures = append(failures, fmt.Errorf("TLS material for %s: %w", d.Hostname, err))
		}
	}
	return errors.Join(failures...)
}

package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"net"
	"time"
)

const JobDomainTLS = "proxy.domain.tls"

type TLSStatus struct {
	DomainID      string `json:"domain_id"`
	Hostname      string `json:"hostname"`
	Enabled       bool   `json:"enabled"`
	Expires       string `json:"expires,omitempty"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	VerifiedAt    string `json:"verified_at,omitempty"`
	CAFingerprint string `json:"ca_fingerprint,omitempty"`
	Trust         string `json:"trust"`
}

func (s *TLSService) Type() string { return JobDomainTLS }
func (s *TLSService) Status(ctx context.Context, id string) (TLSStatus, error) {
	d, err := s.network.repo.DomainByID(ctx, id)
	if err != nil {
		return TLSStatus{}, err
	}
	out := TLSStatus{DomainID: id, Hostname: d.Hostname, Enabled: d.TLSEnabled, Trust: "Client trust is not inspected. Install only the exported public CA certificate on intended development clients."}
	r, err := s.record(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil {
		out.Expires = r.NotAfter
		out.Fingerprint = r.Fingerprint
		out.VerifiedAt = r.VerifiedAt
	}
	ca, _, err := s.authority(ctx, false)
	if err == nil {
		out.CAFingerprint = fingerprint(ca.Raw)
	}
	return out, nil
}
func (s *TLSService) Enqueue(ctx context.Context, id, action string, actor, remote *string) (domain.Job, error) {
	switch action {
	case "enable", "renew", "disable", "verify":
	default:
		return domain.Job{}, fmt.Errorf("%w: TLS action must be enable, renew, disable or verify", ErrInvalidInput)
	}
	d, err := s.network.repo.DomainByID(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if action == "enable" || action == "renew" {
		if !localCertificateHostname(d.Hostname) {
			return domain.Job{}, fmt.Errorf("%w: local HTTPS requires a local development hostname", ErrInvalidInput)
		}
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE type=? AND status IN ('queued','running') AND json_extract(payload_json,'$.domain_id')=?`, JobDomainTLS, id).Scan(&count); err != nil {
		return domain.Job{}, err
	}
	if count > 0 {
		return domain.Job{}, fmt.Errorf("%w: TLS operation for this domain is already queued or running", ErrConflict)
	}
	job, err := s.runner.Enqueue(ctx, jobs.Request{Type: JobDomainTLS, ResourceKey: "global:maintenance", RequestedBy: actor, Payload: map[string]any{"domain_id": id, "action": action}})
	if err == nil && s.audit != nil {
		_ = s.audit.Record(ctx, actor, "proxy.tls."+action+".queued", "domain", &id, map[string]any{"job_id": job.ID}, remote)
	}
	return job, err
}
func domainRoute(d Domain) providers.ProxyRoute {
	r := routeFor(d.Hostname, d.TargetPort)
	r.TLS = d.TLSEnabled
	return r
}
func (s *TLSService) log(ctx context.Context, id, stage string) {
	if l, ok := s.runner.(interface {
		Log(context.Context, string, string, string, map[string]any) error
	}); ok {
		_ = l.Log(ctx, id, "info", stage, nil)
	}
}
func (s *TLSService) Run(parent context.Context, job domain.Job) (result map[string]any, runErr error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	s.network.mu.Lock()
	defer s.network.mu.Unlock()
	id, _ := job.Payload["domain_id"].(string)
	action, _ := job.Payload["action"].(string)
	d, err := s.network.repo.DomainByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if action == "verify" {
		if !d.TLSEnabled {
			return nil, fmt.Errorf("HTTPS is not enabled")
		}
		r, err := s.record(ctx, id)
		if err != nil {
			return nil, err
		}
		ca, _, err := s.authority(ctx, false)
		if err != nil {
			return nil, err
		}
		if err = s.verify(ctx, d.Hostname, r.Fingerprint, ca); err != nil {
			return nil, err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err = s.db.ExecContext(ctx, `UPDATE domain_certificates SET verified_at=? WHERE domain_id=?`, now, id); err != nil {
			return nil, err
		}
		return map[string]any{"domain_id": id, "verified": true, "verified_at": now}, nil
	}
	if action != "enable" && action != "renew" && action != "disable" {
		return nil, fmt.Errorf("invalid TLS action")
	}
	oldRoute := domainRoute(d)
	var next certificateRecord
	nextRoute := domainRoute(d)
	nextRoute.TLS = action != "disable"
	if nextRoute.TLS {
		s.log(ctx, job.ID, "proxy.tls.issuing")
		next, err = s.issue(ctx, d)
		if err != nil {
			return nil, err
		}
		certificate, key, err := s.writeMaterial(ctx, next)
		if err != nil {
			return nil, err
		}
		nextRoute.Metadata = map[string]string{"tls_certificate": certificate, "tls_key": key}
	}
	activated, committed := false, false
	defer func() {
		if recover() != nil {
			runErr = fmt.Errorf("TLS operation panicked; restoring committed configuration")
		}
		if committed || !activated {
			return
		}
		rollback, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := s.network.nginx.Apply(rollback, oldRoute); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("TLS rollback failed; retained certificate material for recovery: %w", err))
			return
		}
		if next.KeyName != "" {
			_ = s.secrets.Delete(rollback, tlsScope, next.KeyName)
		}
	}()
	s.log(ctx, job.ID, "proxy.tls.activating")
	// Apply has its own atomic nginx candidate rollback. The outer rollback also
	// protects against failure after activation but before the SQLite commit.
	activated = true
	if err = s.network.nginx.Apply(ctx, nextRoute); err != nil {
		return nil, err
	}
	if nextRoute.TLS {
		ca, _, err := s.authority(ctx, false)
		if err != nil {
			return nil, err
		}
		s.log(ctx, job.ID, "proxy.tls.verifying")
		if err = s.verify(ctx, d.Hostname, next.Fingerprint, ca); err != nil {
			return nil, err
		}
		next.VerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if nextRoute.TLS {
		_, err = tx.ExecContext(ctx, `INSERT INTO domain_certificates(domain_id,hostname,certificate_pem,key_secret_name,fingerprint,not_after,verified_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(domain_id) DO UPDATE SET hostname=excluded.hostname,certificate_pem=excluded.certificate_pem,key_secret_name=excluded.key_secret_name,fingerprint=excluded.fingerprint,not_after=excluded.not_after,verified_at=excluded.verified_at`, id, next.Hostname, next.PEM, next.KeyName, next.Fingerprint, next.NotAfter, next.VerifiedAt)
		if err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE domains SET tls_enabled=?,updated_at=? WHERE id=?`, nextRoute.TLS, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	if s.audit != nil {
		_ = s.audit.Record(ctx, job.RequestedBy, "proxy.tls."+action, "domain", &id, map[string]any{"hostname": d.Hostname, "fingerprint": next.Fingerprint}, nil)
	}
	s.log(ctx, job.ID, "proxy.tls.completed")
	return map[string]any{"domain_id": id, "https_enabled": nextRoute.TLS, "hostname": d.Hostname, "expires": next.NotAfter, "verified_at": next.VerifiedAt}, nil
}
func verifyTLSListener(parent context.Context, hostname, expected string, ca *x509.Certificate) error {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 2 * time.Second}, Config: &tls.Config{RootCAs: roots, ServerName: hostname, MinVersion: tls.VersionTLS12}}
	for {
		conn, err := dialer.DialContext(ctx, "tcp", "127.0.0.1:443")
		if err == nil {
			state := conn.(*tls.Conn).ConnectionState()
			_ = conn.Close()
			if len(state.PeerCertificates) > 0 && fingerprint(state.PeerCertificates[0].Raw) == expected {
				return nil
			}
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("HTTPS listener did not present the expected trusted certificate for %s: %w", hostname, ctx.Err())
		case <-timer.C:
		}
	}
}

// After a crash, the committed database is authoritative. Never replay issuance
// blindly: reconcile nginx to that committed certificate/HTTP configuration.
func (s *TLSService) RecoverInterrupted(ctx context.Context, job domain.Job) (bool, error) {
	id, _ := job.Payload["domain_id"].(string)
	d, err := s.network.repo.DomainByID(ctx, id)
	if err != nil {
		return false, err
	}
	s.network.mu.Lock()
	defer s.network.mu.Unlock()
	err = s.network.nginx.Apply(ctx, domainRoute(d))
	return false, err
}
func (s *TLSService) StartRenewalScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			s.queueDue(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *TLSService) queueDue(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT c.domain_id,c.not_after FROM domain_certificates c JOIN domains d ON d.id=c.domain_id WHERE d.tls_enabled=1`)
	if err != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id, expiry string
		if err = rows.Scan(&id, &expiry); err != nil {
			break
		}
		t, e := time.Parse(time.RFC3339Nano, expiry)
		if e == nil && time.Until(t) < 30*24*time.Hour {
			ids = append(ids, id)
		}
	}
	rowErr := rows.Err()
	_ = rows.Close()
	if err != nil || rowErr != nil {
		return
	}
	for _, id := range ids {
		_, _ = s.Enqueue(ctx, id, "renew", nil, nil)
	}
}

package proxy

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id, d.project_id, COALESCE(p.name,''), d.hostname, d.target_port,
		       d.tls_enabled, d.created_at, d.updated_at,
		       h.type, h.target, h.last_status, h.last_response_ms, h.last_error, h.last_checked_at
		FROM domains d
		JOIN projects p ON p.id = d.project_id
		LEFT JOIN health_checks h ON h.id = (
			SELECT hc.id
			FROM health_checks hc
			WHERE hc.project_id = d.project_id
			  AND hc.target IN (
			      'http://127.0.0.1:' || d.target_port || '/',
			      '127.0.0.1:' || d.target_port
			  )
			ORDER BY hc.last_checked_at DESC
			LIMIT 1
		)
		ORDER BY d.hostname
	`)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()

	out := make([]Domain, 0)
	for rows.Next() {
		d, err := scanDomain(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *SQLiteRepository) DomainByID(ctx context.Context, id string) (Domain, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT d.id, d.project_id, COALESCE(p.name,''), d.hostname, d.target_port,
		       d.tls_enabled, d.created_at, d.updated_at,
		       h.type, h.target, h.last_status, h.last_response_ms, h.last_error, h.last_checked_at
		FROM domains d
		JOIN projects p ON p.id = d.project_id
		LEFT JOIN health_checks h ON h.id = (
			SELECT hc.id
			FROM health_checks hc
			WHERE hc.project_id = d.project_id
			  AND hc.target IN (
			      'http://127.0.0.1:' || d.target_port || '/',
			      '127.0.0.1:' || d.target_port
			  )
			ORDER BY hc.last_checked_at DESC
			LIMIT 1
		)
		WHERE d.id = ?
	`, id)
	d, err := scanDomain(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		return Domain{}, fmt.Errorf("get domain: %w", err)
	}
	return d, nil
}

func (r *SQLiteRepository) DomainByHostname(ctx context.Context, hostname string) (Domain, error) {
	var id string
	err := r.db.QueryRowContext(ctx, "SELECT id FROM domains WHERE hostname = ?", hostname).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		return Domain{}, fmt.Errorf("find domain: %w", err)
	}
	return r.DomainByID(ctx, id)
}

func (r *SQLiteRepository) CreateDomain(ctx context.Context, projectID, hostname string, targetPort int) (Domain, error) {
	now := time.Now().UTC()
	id := newID()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO domains(id, project_id, hostname, target_port, tls_enabled, config_json, created_at, updated_at)
		VALUES(?,?,?,?,0,'{}',?,?)
	`, id, projectID, hostname, targetPort, formatTime(now), formatTime(now))
	if err != nil {
		return Domain{}, classifyWriteError("create domain", err)
	}
	return r.DomainByID(ctx, id)
}

func (r *SQLiteRepository) UpdateDomain(ctx context.Context, id, hostname string, targetPort int) (Domain, error) {
	res, err := r.db.ExecContext(ctx,
		"UPDATE domains SET hostname = ?, target_port = ?, updated_at = ? WHERE id = ?",
		hostname, targetPort, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return Domain{}, classifyWriteError("update domain", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Domain{}, fmt.Errorf("update domain rows: %w", err)
	}
	if n == 0 {
		return Domain{}, ErrNotFound
	}
	return r.DomainByID(ctx, id)
}

func (r *SQLiteRepository) DeleteDomain(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM domains WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete domain rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) StoreHealth(ctx context.Context, projectID, checkType, target string, result HealthResult, timeout time.Duration) error {
	now := result.CheckedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	message := "ok"
	var lastError any
	if result.Error != "" {
		message = result.Error
		lastError = result.Error
	}
	timeoutSeconds := int(timeout.Seconds())
	if timeoutSeconds < 1 {
		timeoutSeconds = 1
	}

	res, err := r.db.ExecContext(ctx, `
		UPDATE health_checks
		SET timeout_seconds = ?, enabled = 1, last_status = ?, last_message = ?,
		    last_response_ms = ?, last_error = ?, last_checked_at = ?, updated_at = ?
		WHERE project_id = ? AND type = ? AND target = ?
	`, timeoutSeconds, result.Status, message, result.ResponseTimeMS, lastError,
		formatTime(now), formatTime(now), projectID, checkType, target)
	if err != nil {
		return fmt.Errorf("update health check: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update health check rows: %w", err)
	}
	if n > 0 {
		return nil
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO health_checks(
			id, project_id, type, target, interval_seconds, timeout_seconds, enabled,
			last_status, last_message, last_response_ms, last_error, last_checked_at,
			created_at, updated_at
		)
		VALUES(?,?,?,?,30,?,1,?,?,?,?,?,?,?)
	`, newID(), projectID, checkType, target, timeoutSeconds, result.Status, message,
		result.ResponseTimeMS, lastError, formatTime(now), formatTime(now), formatTime(now))
	if err != nil {
		return classifyWriteError("store health check", err)
	}
	return nil
}

type rowScanner func(dest ...any) error

func scanDomain(scan rowScanner) (Domain, error) {
	var d Domain
	var tls int
	var created, updated string
	var healthType, healthTarget, healthStatus, healthError, healthChecked sql.NullString
	var responseMS sql.NullInt64
	if err := scan(
		&d.ID, &d.ProjectID, &d.Application, &d.Hostname, &d.TargetPort,
		&tls, &created, &updated,
		&healthType, &healthTarget, &healthStatus, &responseMS, &healthError, &healthChecked,
	); err != nil {
		return Domain{}, err
	}
	d.TLSEnabled = tls != 0
	d.Target = fmt.Sprintf("127.0.0.1:%d", d.TargetPort)
	d.Status = "configured"
	var err error
	d.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return Domain{}, err
	}
	d.UpdatedAt, err = parseDBTime(updated)
	if err != nil {
		return Domain{}, err
	}
	if healthStatus.Valid {
		checkedAt := time.Time{}
		if healthChecked.Valid {
			checkedAt, err = parseDBTime(healthChecked.String)
			if err != nil {
				return Domain{}, err
			}
		}
		h := &HealthResult{
			Status:         healthStatus.String,
			ResponseTimeMS: responseMS.Int64,
			CheckedAt:      checkedAt,
			Type:           healthType.String,
			Target:         healthTarget.String,
			ProjectID:      d.ProjectID,
		}
		if healthError.Valid {
			h.Error = healthError.String
		}
		d.Health = h
	}
	return d, nil
}

func classifyWriteError(operation string, err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unique constraint"):
		return fmt.Errorf("%w: %s", ErrConflict, operation)
	case strings.Contains(msg, "foreign key constraint"):
		return fmt.Errorf("%w: referenced project does not exist", ErrInvalidInput)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	return hex.EncodeToString(buf)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(value string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range formats {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported database timestamp %q", value)
}

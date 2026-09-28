package apphealth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CurrentCheck struct {
	ID              string  `json:"id"`
	ProjectID       string  `json:"project_id"`
	ProjectName     string  `json:"project_name"`
	Type            string  `json:"type"`
	Target          string  `json:"target"`
	IntervalSeconds int     `json:"interval_seconds"`
	TimeoutSeconds  int     `json:"timeout_seconds"`
	Enabled         bool    `json:"enabled"`
	Status          string  `json:"status"`
	Message         string  `json:"message,omitempty"`
	ResponseTimeMS  int64   `json:"response_time_ms"`
	Error           string  `json:"error,omitempty"`
	CheckedAt       *string `json:"checked_at,omitempty"`
}

type HistoryEntry struct {
	ID             int64  `json:"id"`
	CheckID        string `json:"check_id"`
	ProjectID      string `json:"project_id"`
	ProjectName    string `json:"project_name"`
	Status         string `json:"status"`
	ResponseTimeMS int64  `json:"response_time_ms"`
	Message        string `json:"message,omitempty"`
	Error          string `json:"error,omitempty"`
	CheckedAt      string `json:"checked_at"`
}

type checkResult struct {
	Status         string
	Message        string
	Error          string
	ResponseTimeMS int64
	CheckedAt      time.Time
}

type projectTarget struct {
	ID          string
	Name        string
	Healthcheck string
	Port        sql.NullInt64
}

type Service struct {
	db            *sql.DB
	interval      time.Duration
	timeout       time.Duration
	retentionDays int
	httpClient    *http.Client
}

func NewService(db *sql.DB, interval, timeout time.Duration, retentionDays int) *Service {
	if interval < 5*time.Second {
		interval = 30 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if retentionDays < 1 {
		retentionDays = 30
	}
	return &Service{
		db:            db,
		interval:      interval,
		timeout:       timeout,
		retentionDays: retentionDays,
		httpClient:    &http.Client{Timeout: timeout},
	}
}

func (s *Service) Run(ctx context.Context) {
	_ = s.RunDue(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.RunDue(ctx)
		}
	}
}

func (s *Service) RunDue(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.healthcheck,
		       (SELECT port FROM ports WHERE project_id=p.id AND released_at IS NULL ORDER BY created_at DESC LIMIT 1)
		FROM projects p
		WHERE p.archived_at IS NULL AND TRIM(COALESCE(p.healthcheck,'')) <> ''
		ORDER BY p.id
	`)
	if err != nil {
		return fmt.Errorf("list health targets: %w", err)
	}
	defer rows.Close()

	targets := make([]projectTarget, 0)
	for rows.Next() {
		var item projectTarget
		if err := rows.Scan(&item.ID, &item.Name, &item.Healthcheck, &item.Port); err != nil {
			return fmt.Errorf("scan health target: %w", err)
		}
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate health targets: %w", err)
	}

	for _, target := range targets {
		if _, err := s.runTarget(ctx, target); err != nil && !errors.Is(err, context.Canceled) {
			continue
		}
	}
	return s.cleanupHistory(ctx)
}

func (s *Service) RunProject(ctx context.Context, projectID string) (CurrentCheck, error) {
	var target projectTarget
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.healthcheck,
		       (SELECT port FROM ports WHERE project_id=p.id AND released_at IS NULL ORDER BY created_at DESC LIMIT 1)
		FROM projects p
		WHERE p.id=? AND p.archived_at IS NULL
	`, projectID).Scan(&target.ID, &target.Name, &target.Healthcheck, &target.Port)
	if errors.Is(err, sql.ErrNoRows) {
		return CurrentCheck{}, errors.New("project not found")
	}
	if err != nil {
		return CurrentCheck{}, fmt.Errorf("get project health target: %w", err)
	}
	if strings.TrimSpace(target.Healthcheck) == "" {
		return CurrentCheck{}, errors.New("project does not define a healthcheck")
	}
	if _, err := s.runTarget(ctx, target); err != nil {
		return CurrentCheck{}, err
	}
	return s.currentByProject(ctx, projectID)
}

func (s *Service) runTarget(ctx context.Context, project projectTarget) (checkResult, error) {
	checkType, target, err := resolveTarget(project.Healthcheck, project.Port)
	if err != nil {
		result := checkResult{
			Status:    "unhealthy",
			Error:     err.Error(),
			Message:   err.Error(),
			CheckedAt: time.Now().UTC(),
		}
		if storeErr := s.storeResult(ctx, project, "invalid", strings.TrimSpace(project.Healthcheck), result); storeErr != nil {
			return result, storeErr
		}
		return result, nil
	}

	var result checkResult
	switch checkType {
	case "http":
		result = s.checkHTTP(ctx, target)
	case "tcp":
		result = s.checkTCP(ctx, target)
	default:
		return checkResult{}, fmt.Errorf("unsupported healthcheck type %q", checkType)
	}
	if err := s.storeResult(ctx, project, checkType, target, result); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) checkHTTP(ctx context.Context, target string) checkResult {
	started := time.Now()
	result := checkResult{Status: "unhealthy", CheckedAt: started.UTC()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		result.Error = err.Error()
		result.Message = err.Error()
		return result
	}
	resp, err := s.httpClient.Do(req)
	result.ResponseTimeMS = time.Since(started).Milliseconds()
	result.CheckedAt = time.Now().UTC()
	if err != nil {
		result.Error = err.Error()
		result.Message = err.Error()
		return result
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	result.Message = resp.Status
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Status = "healthy"
	}
	return result
}

func (s *Service) checkTCP(ctx context.Context, target string) checkResult {
	started := time.Now()
	result := checkResult{Status: "unhealthy", CheckedAt: started.UTC()}
	dialer := net.Dialer{Timeout: s.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	result.ResponseTimeMS = time.Since(started).Milliseconds()
	result.CheckedAt = time.Now().UTC()
	if err != nil {
		result.Error = err.Error()
		result.Message = err.Error()
		return result
	}
	_ = conn.Close()
	result.Status = "healthy"
	result.Message = "TCP connection succeeded"
	return result
}

func resolveTarget(configured string, port sql.NullInt64) (string, string, error) {
	value := strings.TrimSpace(configured)
	if value == "" {
		return "", "", errors.New("healthcheck target is empty")
	}
	if strings.HasPrefix(value, "/") {
		if !port.Valid || port.Int64 < 1 || port.Int64 > 65535 {
			return "", "", errors.New("relative HTTP healthcheck requires an allocated project port")
		}
		return "http", "http://127.0.0.1:" + strconv.FormatInt(port.Int64, 10) + value, nil
	}
	if strings.HasPrefix(strings.ToLower(value), "tcp://") {
		target := strings.TrimSpace(value[len("tcp://"):])
		if _, _, err := net.SplitHostPort(target); err != nil {
			return "", "", fmt.Errorf("invalid TCP healthcheck target: %w", err)
		}
		return "tcp", target, nil
	}
	parsed, err := url.Parse(value)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		return "http", parsed.String(), nil
	}
	if _, _, err := net.SplitHostPort(value); err == nil {
		return "tcp", value, nil
	}
	return "", "", errors.New("healthcheck must be an http(s) URL, tcp://host:port, host:port, or a relative path such as /health")
}

func (s *Service) storeResult(ctx context.Context, project projectTarget, checkType, target string, result checkResult) error {
	intervalSeconds := int(s.interval.Seconds())
	timeoutSeconds := int(s.timeout.Seconds())
	if intervalSeconds < 1 {
		intervalSeconds = 1
	}
	if timeoutSeconds < 1 {
		timeoutSeconds = 1
	}
	now := result.CheckedAt.UTC().Format(time.RFC3339Nano)
	checkID := newID()
	if _, err := s.db.ExecContext(ctx,
		"UPDATE health_checks SET enabled=0,updated_at=? WHERE project_id=? AND NOT (type=? AND target=?)",
		now, project.ID, checkType, target,
	); err != nil {
		return fmt.Errorf("disable superseded health checks: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO health_checks(
			id, project_id, type, target, interval_seconds, timeout_seconds, enabled,
			last_status, last_message, last_response_ms, last_error, last_checked_at, created_at, updated_at
		)
		VALUES(?,?,?,?,?,?,1,?,?,?,?,?,?,?)
		ON CONFLICT(project_id,type,target) DO UPDATE SET
			interval_seconds=excluded.interval_seconds,
			timeout_seconds=excluded.timeout_seconds,
			enabled=1,
			last_status=excluded.last_status,
			last_message=excluded.last_message,
			last_response_ms=excluded.last_response_ms,
			last_error=excluded.last_error,
			last_checked_at=excluded.last_checked_at,
			updated_at=excluded.updated_at
	`, checkID, project.ID, checkType, target, intervalSeconds, timeoutSeconds,
		result.Status, nullable(result.Message), result.ResponseTimeMS, nullable(result.Error),
		now, now, now)
	if err != nil {
		return fmt.Errorf("persist health check: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		"SELECT id FROM health_checks WHERE project_id=? AND type=? AND target=?",
		project.ID, checkType, target,
	).Scan(&checkID); err != nil {
		return fmt.Errorf("resolve health check id: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO health_check_history(check_id,project_id,status,response_time_ms,message,error,checked_at)
		VALUES(?,?,?,?,?,?,?)
	`, checkID, project.ID, result.Status, result.ResponseTimeMS, nullable(result.Message), nullable(result.Error), now); err != nil {
		return fmt.Errorf("persist health history: %w", err)
	}
	return nil
}

func (s *Service) ListCurrent(ctx context.Context) ([]CurrentCheck, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT h.id,h.project_id,p.name,h.type,h.target,h.interval_seconds,h.timeout_seconds,h.enabled,
		       COALESCE(h.last_status,''),COALESCE(h.last_message,''),COALESCE(h.last_response_ms,0),
		       COALESCE(h.last_error,''),h.last_checked_at
		FROM health_checks h
		JOIN projects p ON p.id=h.project_id
		WHERE p.archived_at IS NULL AND h.enabled=1
		ORDER BY p.name,h.type,h.target
	`)
	if err != nil {
		return nil, fmt.Errorf("list health checks: %w", err)
	}
	defer rows.Close()
	out := make([]CurrentCheck, 0)
	for rows.Next() {
		item, err := scanCurrent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) currentByProject(ctx context.Context, projectID string) (CurrentCheck, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT h.id,h.project_id,p.name,h.type,h.target,h.interval_seconds,h.timeout_seconds,h.enabled,
		       COALESCE(h.last_status,''),COALESCE(h.last_message,''),COALESCE(h.last_response_ms,0),
		       COALESCE(h.last_error,''),h.last_checked_at
		FROM health_checks h
		JOIN projects p ON p.id=h.project_id
		WHERE h.project_id=? AND h.enabled=1
		ORDER BY h.last_checked_at DESC
		LIMIT 1
	`, projectID)
	return scanCurrent(row.Scan)
}

type scanner func(dest ...any) error

func scanCurrent(scan scanner) (CurrentCheck, error) {
	var item CurrentCheck
	var enabled int
	var checked sql.NullString
	if err := scan(
		&item.ID, &item.ProjectID, &item.ProjectName, &item.Type, &item.Target,
		&item.IntervalSeconds, &item.TimeoutSeconds, &enabled, &item.Status, &item.Message,
		&item.ResponseTimeMS, &item.Error, &checked,
	); err != nil {
		return CurrentCheck{}, err
	}
	item.Enabled = enabled != 0
	if checked.Valid {
		item.CheckedAt = &checked.String
	}
	return item, nil
}

func (s *Service) History(ctx context.Context, projectID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT hh.id,hh.check_id,hh.project_id,p.name,hh.status,hh.response_time_ms,
		       COALESCE(hh.message,''),COALESCE(hh.error,''),hh.checked_at
		FROM health_check_history hh
		JOIN projects p ON p.id=hh.project_id
		WHERE (?='' OR hh.project_id=?)
		ORDER BY hh.checked_at DESC
		LIMIT ?
	`, projectID, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("list health history: %w", err)
	}
	defer rows.Close()
	out := make([]HistoryEntry, 0, limit)
	for rows.Next() {
		var item HistoryEntry
		if err := rows.Scan(
			&item.ID, &item.CheckID, &item.ProjectID, &item.ProjectName, &item.Status,
			&item.ResponseTimeMS, &item.Message, &item.Error, &item.CheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan health history: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) cleanupHistory(ctx context.Context) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -s.retentionDays).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, "DELETE FROM health_check_history WHERE checked_at < ?", cutoff); err != nil {
		return fmt.Errorf("prune health history: %w", err)
	}
	return nil
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	return hex.EncodeToString(buf)
}

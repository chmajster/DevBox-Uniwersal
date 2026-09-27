package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type HealthChecker struct {
	repo *SQLiteRepository
}

func NewHealthChecker(repo *SQLiteRepository) *HealthChecker {
	return &HealthChecker{repo: repo}
}

func (h *HealthChecker) CheckHTTP(ctx context.Context, target string, timeout time.Duration) HealthResult {
	started := time.Now()
	result := HealthResult{Status: "unhealthy", Type: "http", Target: target}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		result.Error = "invalid HTTP target"
		result.CheckedAt = time.Now().UTC()
		return result
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		result.Error = err.Error()
		result.CheckedAt = time.Now().UTC()
		return result
	}
	resp, err := client.Do(req)
	result.ResponseTimeMS = time.Since(started).Milliseconds()
	result.CheckedAt = time.Now().UTC()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		result.Error = "HTTP status " + strconv.Itoa(resp.StatusCode)
		return result
	}
	result.Status = "healthy"
	return result
}

func (h *HealthChecker) CheckTCP(ctx context.Context, target string, timeout time.Duration) HealthResult {
	started := time.Now()
	result := HealthResult{Status: "unhealthy", Type: "tcp", Target: target}
	if _, _, err := net.SplitHostPort(target); err != nil {
		result.Error = "invalid TCP target"
		result.CheckedAt = time.Now().UTC()
		return result
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	result.ResponseTimeMS = time.Since(started).Milliseconds()
	result.CheckedAt = time.Now().UTC()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	_ = conn.Close()
	result.Status = "healthy"
	return result
}

func (h *HealthChecker) RunAndStore(ctx context.Context, projectID, checkType, target string, timeout time.Duration) (HealthResult, error) {
	if strings.TrimSpace(projectID) == "" {
		return HealthResult{}, fmt.Errorf("%w: project_id is required", ErrInvalidInput)
	}
	if timeout <= 0 {
		return HealthResult{}, fmt.Errorf("%w: timeout must be greater than zero", ErrInvalidInput)
	}

	var result HealthResult
	switch strings.ToLower(strings.TrimSpace(checkType)) {
	case "http":
		result = h.CheckHTTP(ctx, target, timeout)
	case "tcp":
		result = h.CheckTCP(ctx, target, timeout)
	default:
		return HealthResult{}, fmt.Errorf("%w: health type must be http or tcp", ErrInvalidInput)
	}
	result.ProjectID = projectID
	if h.repo == nil {
		return result, nil
	}
	if err := h.repo.StoreHealth(ctx, projectID, result.Type, target, result, timeout); err != nil {
		return HealthResult{}, err
	}
	return result, nil
}

package secrets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveMasterKey returns the configured master key, a persisted local key,
// or creates a new persisted key only when no encrypted secrets exist yet.
// This prevents silently orphaning previously encrypted data.
func ResolveMasterKey(ctx context.Context, db *sql.DB, configured, path string) (string, error) {
	if value := strings.TrimSpace(configured); value != "" {
		return value, nil
	}
	if path == "" {
		return "", errors.New("master key path is empty")
	}
	if raw, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(raw))
		if value == "" {
			return "", errors.New("persisted master key is empty")
		}
		if _, err := NewAESGCMFromBase64(value); err != nil {
			return "", fmt.Errorf("invalid persisted master key: %w", err)
		}
		return value, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read persisted master key: %w", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM secrets").Scan(&count); err != nil {
		return "", fmt.Errorf("count encrypted secrets: %w", err)
	}
	if count > 0 {
		return "", errors.New("DEVBOX_MASTER_KEY is missing while encrypted secrets already exist; restore the original master key")
	}

	value, err := GenerateMasterKey()
	if err != nil {
		return "", fmt.Errorf("generate master key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("create master key directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist master key: %w", err)
	}
	return value, nil
}

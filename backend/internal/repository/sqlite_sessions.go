package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type SQLiteSessions struct{ db *sql.DB }

func NewSQLiteSessions(db *sql.DB) *SQLiteSessions { return &SQLiteSessions{db: db} }

func (r *SQLiteSessions) Create(ctx context.Context, s domain.Session) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO sessions(id, user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`, s.ID, s.UserID, s.TokenHash, s.ExpiresAt.UTC().Format(time.RFC3339Nano), s.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *SQLiteSessions) ByTokenHash(ctx context.Context, hash string) (domain.Session, error) {
	var s domain.Session
	var expires, created string
	err := r.db.QueryRowContext(ctx, `SELECT id, user_id, token_hash, expires_at, created_at FROM sessions WHERE token_hash = ?`, hash).Scan(&s.ID, &s.UserID, &s.TokenHash, &expires, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Session{}, ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("query session: %w", err)
	}
	s.ExpiresAt, err = parseTime(expires)
	if err != nil {
		return domain.Session{}, err
	}
	s.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.Session{}, err
	}
	return s, nil
}

func (r *SQLiteSessions) DeleteByTokenHash(ctx context.Context, hash string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *SQLiteSessions) DeleteExpired(ctx context.Context, now time.Time) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}

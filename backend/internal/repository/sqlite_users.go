package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type SQLiteUsers struct{ db *sql.DB }

func NewSQLiteUsers(db *sql.DB) *SQLiteUsers { return &SQLiteUsers{db: db} }

func (r *SQLiteUsers) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM users").Scan(&count); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return count, nil
}

func (r *SQLiteUsers) CountActiveAdmins(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM users WHERE role = 'admin' AND active = 1").Scan(&count); err != nil {
		return 0, fmt.Errorf("count active admins: %w", err)
	}
	return count, nil
}

func (r *SQLiteUsers) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, username, password_hash, role, active, created_at, updated_at FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	items := make([]domain.User, 0)
	for rows.Next() {
		var u domain.User
		var role string
		var active int
		var created, updated string
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &active, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.Role = domain.Role(role)
		u.Active = active == 1
		u.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		u.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, err
		}
		items = append(items, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return items, nil
}

func (r *SQLiteUsers) Create(ctx context.Context, user domain.User) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO users(id, username, password_hash, role, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, user.ID, user.Username, user.PasswordHash, string(user.Role), boolInt(user.Active), user.CreatedAt.UTC().Format(time.RFC3339Nano), user.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *SQLiteUsers) ByID(ctx context.Context, id string) (domain.User, error) {
	return r.byQuery(ctx, `SELECT id, username, password_hash, role, active, created_at, updated_at FROM users WHERE id = ?`, id)
}

func (r *SQLiteUsers) ByUsername(ctx context.Context, username string) (domain.User, error) {
	return r.byQuery(ctx, `SELECT id, username, password_hash, role, active, created_at, updated_at FROM users WHERE username = ? COLLATE NOCASE`, username)
}

func (r *SQLiteUsers) byQuery(ctx context.Context, query string, arg any) (domain.User, error) {
	var u domain.User
	var role string
	var active int
	var created, updated string
	err := r.db.QueryRowContext(ctx, query, arg).Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &active, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("query user: %w", err)
	}
	u.Role = domain.Role(role)
	u.Active = active == 1
	u.CreatedAt, err = parseTime(created)
	if err != nil {
		return domain.User{}, err
	}
	u.UpdatedAt, err = parseTime(updated)
	if err != nil {
		return domain.User{}, err
	}
	return u, nil
}

func (r *SQLiteUsers) UpdatePasswordHash(ctx context.Context, id, passwordHash string, updatedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, updatedAt.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update user password rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteUsers) UpdateRoleAndActive(ctx context.Context, id string, role domain.Role, active bool, updatedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE users SET role = ?, active = ?, updated_at = ? WHERE id = ?`, string(role), boolInt(active), updatedAt.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update user rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteUsers) Delete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete user rows: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func parseTime(v string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, v)
	if err == nil {
		return parsed, nil
	}
	parsed, err = time.Parse("2006-01-02 15:04:05", v)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse sqlite time %q: %w", v, err)
	}
	return parsed.UTC(), nil
}

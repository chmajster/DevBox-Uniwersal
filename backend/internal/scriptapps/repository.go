package scriptapps

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("script app not found")

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (r *Repository) Create(ctx context.Context, a App) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO script_apps(
		id,name,description,install_source,update_source,uninstall_source,interpreter,checksum_sha256,
		run_as_root,allow_insecure,manager,manager_target,status,last_error,created_by,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.Description, a.InstallSource, a.UpdateSource, a.UninstallSource, a.Interpreter, a.ChecksumSHA256,
		boolInt(a.RunAsRoot), boolInt(a.AllowInsecure), a.Manager, a.ManagerTarget, a.Status, a.LastError, a.CreatedBy, formatTime(a.CreatedAt), formatTime(a.UpdatedAt))
	return err
}

func (r *Repository) List(ctx context.Context) ([]App, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,description,install_source,update_source,uninstall_source,interpreter,checksum_sha256,
		run_as_root,allow_insecure,manager,manager_target,status,last_error,created_by,created_at,updated_at
		FROM script_apps ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]App, 0)
	for rows.Next() {
		a, err := scan(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (App, error) {
	a, err := scan(r.db.QueryRowContext(ctx, `SELECT id,name,description,install_source,update_source,uninstall_source,interpreter,checksum_sha256,
		run_as_root,allow_insecure,manager,manager_target,status,last_error,created_by,created_at,updated_at
		FROM script_apps WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return App{}, ErrNotFound
	}
	return a, err
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM script_apps WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SetState(ctx context.Context, id, status, manager, target, lastError string) error {
	res, err := r.db.ExecContext(ctx, `UPDATE script_apps SET status=?,manager=?,manager_target=?,last_error=?,updated_at=? WHERE id=?`,
		status, manager, target, lastError, formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner func(...any) error

func scan(s scanner) (App, error) {
	var a App
	var root, insecure int
	var createdBy sql.NullString
	var created, updated string
	err := s(&a.ID, &a.Name, &a.Description, &a.InstallSource, &a.UpdateSource, &a.UninstallSource, &a.Interpreter, &a.ChecksumSHA256,
		&root, &insecure, &a.Manager, &a.ManagerTarget, &a.Status, &a.LastError, &createdBy, &created, &updated)
	if err != nil {
		return App{}, err
	}
	a.RunAsRoot = root != 0
	a.AllowInsecure = insecure != 0
	if createdBy.Valid {
		v := createdBy.String
		a.CreatedBy = &v
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return a, nil
}
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

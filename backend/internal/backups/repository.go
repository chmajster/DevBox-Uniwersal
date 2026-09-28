package backups

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("backup not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, item Backup) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO control_plane_backups(id,file_name,status,size_bytes,requested_by,created_at)
		VALUES(?,?,?,?,?,?)
	`, item.ID, item.FileName, item.Status, item.SizeBytes, item.RequestedBy, formatTime(item.CreatedAt))
	if err != nil {
		return fmt.Errorf("create control-plane backup: %w", err)
	}
	return nil
}

func (r *Repository) List(ctx context.Context) ([]Backup, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id,file_name,status,size_bytes,COALESCE(sha256,''),requested_by,COALESCE(error,''),created_at,completed_at
		FROM control_plane_backups
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list control-plane backups: %w", err)
	}
	defer rows.Close()
	items := make([]Backup, 0)
	for rows.Next() {
		item, err := scanBackup(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ByID(ctx context.Context, id string) (Backup, error) {
	item, err := scanBackup(r.db.QueryRowContext(ctx, `
		SELECT id,file_name,status,size_bytes,COALESCE(sha256,''),requested_by,COALESCE(error,''),created_at,completed_at
		FROM control_plane_backups WHERE id=?
	`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Backup{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) UpdateStatus(ctx context.Context, id, status string, size int64, checksum, message string, completed bool) error {
	var completedAt any
	if completed {
		completedAt = formatTime(time.Now().UTC())
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE control_plane_backups
		SET status=?,size_bytes=?,sha256=NULLIF(?,''),error=NULLIF(?,''),completed_at=COALESCE(?,completed_at)
		WHERE id=?
	`, status, size, checksum, message, completedAt, id)
	if err != nil {
		return fmt.Errorf("update control-plane backup: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM control_plane_backups WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("delete control-plane backup: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner func(dest ...any) error

func scanBackup(scan rowScanner) (Backup, error) {
	var item Backup
	var created string
	var completed sql.NullString
	if err := scan(&item.ID,&item.FileName,&item.Status,&item.SizeBytes,&item.SHA256,&item.RequestedBy,&item.Error,&created,&completed); err != nil {
		return Backup{}, err
	}
	var err error
	item.CreatedAt, err = parseTime(created)
	if err != nil {
		return Backup{}, err
	}
	if completed.Valid {
		value, err := parseTime(completed.String)
		if err != nil {
			return Backup{}, err
		}
		item.CompletedAt = &value
	}
	return item, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano,time.RFC3339,"2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout,value); err == nil {
			return parsed.UTC(),nil
		}
	}
	return time.Time{},fmt.Errorf("invalid backup timestamp %q",value)
}

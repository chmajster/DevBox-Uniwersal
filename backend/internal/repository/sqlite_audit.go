package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type SQLiteAudit struct{ db *sql.DB }

func NewSQLiteAudit(db *sql.DB) *SQLiteAudit { return &SQLiteAudit{db: db} }

func (r *SQLiteAudit) Append(ctx context.Context, event domain.AuditEvent) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO audit_events(id,actor_user_id,action,resource_type,resource_id,metadata_json,remote_addr,created_at) VALUES(?,?,?,?,?,?,?,?)`, event.ID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, string(metadata), event.RemoteAddr, event.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

func (r *SQLiteAudit) List(ctx context.Context, limit, offset int) ([]domain.AuditEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,actor_user_id,action,resource_type,resource_id,metadata_json,remote_addr,created_at FROM audit_events ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	var out []domain.AuditEvent
	for rows.Next() {
		var e domain.AuditEvent
		var actor, resource, metadata, remote sql.NullString
		var created string
		if err := rows.Scan(&e.ID, &actor, &e.Action, &e.ResourceType, &resource, &metadata, &remote, &created); err != nil {
			return nil, err
		}
		if actor.Valid {
			e.ActorUserID = &actor.String
		}
		if resource.Valid {
			e.ResourceID = &resource.String
		}
		if remote.Valid {
			e.RemoteAddr = &remote.String
		}
		if metadata.Valid && metadata.String != "" {
			_ = json.Unmarshal([]byte(metadata.String), &e.Metadata)
		}
		e.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

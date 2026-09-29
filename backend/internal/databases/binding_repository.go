package databases

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrBindingNotFound = errors.New("database binding not found")

func (r *Repository) DatabaseBindingByProject(ctx context.Context, projectID string) (DatabaseBinding, error) {
	var item DatabaseBinding
	var databaseID, secretRef sql.NullString
	var hostAccessOnly int
	var created, updated string
	err := r.db.QueryRowContext(ctx, `
		SELECT id,project_id,mode,database_id,application_service,compose_service,engine,
		       connection_host,connection_port,database_name,username,secret_ref,host_access_only,created_at,updated_at
		FROM project_database_bindings WHERE project_id=?`, projectID).
		Scan(&item.ID, &item.ProjectID, &item.Mode, &databaseID, &item.ApplicationService, &item.ComposeService,
			&item.Engine, &item.Host, &item.Port, &item.Database, &item.Username, &secretRef, &hostAccessOnly, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return DatabaseBinding{}, ErrBindingNotFound
	}
	if err != nil {
		return DatabaseBinding{}, fmt.Errorf("get project database binding: %w", err)
	}
	if databaseID.Valid {
		item.DatabaseID = &databaseID.String
	}
	if secretRef.Valid {
		item.SecretRef = secretRef.String
		item.HasSecret = item.SecretRef != ""
	}
	item.HostAccessOnly = hostAccessOnly != 0
	item.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return DatabaseBinding{}, err
	}
	item.UpdatedAt, err = parseDBTime(updated)
	if err != nil {
		return DatabaseBinding{}, err
	}
	return item, nil
}

func (r *Repository) UpsertDatabaseBinding(ctx context.Context, item DatabaseBinding) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO project_database_bindings(
			id,project_id,mode,database_id,application_service,compose_service,engine,
			connection_host,connection_port,database_name,username,secret_ref,host_access_only,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET
			mode=excluded.mode,database_id=excluded.database_id,application_service=excluded.application_service,
			compose_service=excluded.compose_service,engine=excluded.engine,connection_host=excluded.connection_host,
			connection_port=excluded.connection_port,database_name=excluded.database_name,username=excluded.username,
			secret_ref=excluded.secret_ref,host_access_only=excluded.host_access_only,updated_at=excluded.updated_at`,
		item.ID, item.ProjectID, item.Mode, item.DatabaseID, item.ApplicationService, item.ComposeService,
		item.Engine, item.Host, item.Port, item.Database, item.Username, nullableBinding(item.SecretRef), bindingBoolInt(item.HostAccessOnly),
		item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store project database binding: %w", err)
	}
	return nil
}

func (r *Repository) DeleteDatabaseBinding(ctx context.Context, projectID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM project_database_bindings WHERE project_id=?`, projectID)
	if err != nil {
		return fmt.Errorf("delete project database binding: %w", err)
	}
	return nil
}

func bindingBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableBinding(value string) any {
	if value == "" {
		return nil
	}
	return value
}

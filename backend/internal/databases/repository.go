package databases

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("database resource not found")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListDatabases(ctx context.Context) ([]Database, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id,d.project_id,COALESCE(p.name,''),d.provider,d.engine,d.name,d.status,d.created_at,d.updated_at,
		       COALESCE((SELECT da.username FROM database_user_grants dug JOIN database_accounts da ON da.id=dug.user_id WHERE dug.database_id=d.id ORDER BY dug.created_at LIMIT 1),'')
		FROM databases d
		LEFT JOIN projects p ON p.id=d.project_id
		ORDER BY d.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()

	var out []Database
	for rows.Next() {
		var item Database
		var projectID sql.NullString
		var created, updated string
		if err := rows.Scan(&item.ID, &projectID, &item.ApplicationName, &item.Provider, &item.Engine, &item.Name, &item.Status, &created, &updated, &item.Username); err != nil {
			return nil, fmt.Errorf("scan database: %w", err)
		}
		if projectID.Valid {
			item.ProjectID = &projectID.String
		}
		item.CreatedAt, err = parseDBTime(created)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = parseDBTime(updated)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) DatabaseByID(ctx context.Context, id string) (Database, error) {
	var item Database
	var projectID sql.NullString
	var created, updated string
	err := r.db.QueryRowContext(ctx, `
		SELECT d.id,d.project_id,COALESCE(p.name,''),d.provider,d.engine,d.name,d.status,d.created_at,d.updated_at,
		       COALESCE((SELECT da.username FROM database_user_grants dug JOIN database_accounts da ON da.id=dug.user_id WHERE dug.database_id=d.id ORDER BY dug.created_at LIMIT 1),'')
		FROM databases d
		LEFT JOIN projects p ON p.id=d.project_id
		WHERE d.id=?`, id).Scan(&item.ID, &projectID, &item.ApplicationName, &item.Provider, &item.Engine, &item.Name, &item.Status, &created, &updated, &item.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return Database{}, ErrNotFound
	}
	if err != nil {
		return Database{}, fmt.Errorf("get database: %w", err)
	}
	if projectID.Valid {
		item.ProjectID = &projectID.String
	}
	item.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return Database{}, err
	}
	item.UpdatedAt, err = parseDBTime(updated)
	if err != nil {
		return Database{}, err
	}
	return item, nil
}

func (r *Repository) DatabaseByProject(ctx context.Context, projectID string) (Database, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `SELECT id FROM databases WHERE project_id=? ORDER BY created_at LIMIT 1`, projectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Database{}, ErrNotFound
	}
	if err != nil {
		return Database{}, fmt.Errorf("find project database: %w", err)
	}
	return r.DatabaseByID(ctx, id)
}

func (r *Repository) CreateDatabase(ctx context.Context, item Database) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO databases(id,project_id,provider,engine,name,status,config_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?, ?, ?)`,
		item.ID, item.ProjectID, item.Provider, item.Engine, item.Name, item.Status, "{}", item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert database: %w", err)
	}
	return nil
}

func (r *Repository) UpdateDatabaseStatus(ctx context.Context, id, status string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE databases SET status=?,updated_at=? WHERE id=?`, status, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update database status: %w", err)
	}
	return requireAffected(result)
}

func (r *Repository) DeleteDatabase(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM databases WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete database metadata: %w", err)
	}
	return requireAffected(result)
}

func (r *Repository) ProjectByID(ctx context.Context, id string) (ProjectRef, error) {
	var project ProjectRef
	err := r.db.QueryRowContext(ctx, `SELECT id,name,slug,COALESCE(local_path,''),COALESCE(working_directory,'') FROM projects WHERE id=?`, id).Scan(&project.ID, &project.Name, &project.Slug, &project.LocalPath, &project.WorkingDirectory)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectRef{}, ErrNotFound
	}
	if err != nil {
		return ProjectRef{}, fmt.Errorf("get project: %w", err)
	}
	return project, nil
}

func (r *Repository) ListUsers(ctx context.Context) ([]DatabaseUser, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,engine,username,secret_id,created_at,updated_at FROM database_accounts ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list database users: %w", err)
	}

	var out []DatabaseUser
	for rows.Next() {
		item, err := scanDatabaseAccount(rows.Scan)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i], err = r.decorateDatabaseUser(ctx, out[i])
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *Repository) UsersByDatabase(ctx context.Context, databaseID string) ([]DatabaseUser, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT da.id,da.engine,da.username,da.secret_id,da.created_at,da.updated_at
		FROM database_accounts da
		JOIN database_user_grants dug ON dug.user_id=da.id
		WHERE dug.database_id=?
		ORDER BY da.created_at`, databaseID)
	if err != nil {
		return nil, fmt.Errorf("list database users: %w", err)
	}

	var out []DatabaseUser
	for rows.Next() {
		item, err := scanDatabaseAccount(rows.Scan)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i], err = r.decorateDatabaseUser(ctx, out[i])
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *Repository) UserByID(ctx context.Context, id string) (DatabaseUser, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id,engine,username,secret_id,created_at,updated_at FROM database_accounts WHERE id=?`, id)
	item, err := scanDatabaseAccount(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return DatabaseUser{}, ErrNotFound
	}
	if err != nil {
		return DatabaseUser{}, err
	}
	return r.decorateDatabaseUser(ctx, item)
}

func (r *Repository) UserDatabaseGrants(ctx context.Context, userID string) ([]DatabaseUserGrant, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.id,d.name,d.engine,dug.privileges_json
		FROM database_user_grants dug
		JOIN databases d ON d.id=dug.database_id
		WHERE dug.user_id=?
		ORDER BY d.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("list database user grants: %w", err)
	}
	defer rows.Close()

	var out []DatabaseUserGrant
	for rows.Next() {
		var item DatabaseUserGrant
		var privileges string
		if err := rows.Scan(&item.DatabaseID, &item.DatabaseName, &item.Engine, &privileges); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(privileges), &item.Privileges); err != nil {
			return nil, fmt.Errorf("decode database privileges: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) CreateUser(ctx context.Context, item DatabaseUser) error {
	if item.Engine == "" {
		item.Engine = "mysql"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO database_accounts(id,engine,username,secret_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?)`, item.ID, item.Engine, item.Username, item.SecretRef, item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("insert database user: %w", err)
	}
	if item.DatabaseID != "" {
		privileges, err := json.Marshal(item.Privileges)
		if err != nil {
			return fmt.Errorf("marshal database privileges: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO database_user_grants(user_id,database_id,privileges_json,created_at,updated_at)
			VALUES(?,?,?,?,?)`, item.ID, item.DatabaseID, string(privileges), item.CreatedAt.UTC().Format(time.RFC3339Nano), item.UpdatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("insert database user grant: %w", err)
		}
	}
	return tx.Commit()
}

func (r *Repository) UpsertUserDatabaseGrant(ctx context.Context, userID, databaseID string, privileges []string) error {
	payload, err := json.Marshal(privileges)
	if err != nil {
		return fmt.Errorf("marshal database privileges: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO database_user_grants(user_id,database_id,privileges_json,created_at,updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(user_id,database_id) DO UPDATE SET privileges_json=excluded.privileges_json,updated_at=excluded.updated_at`,
		userID, databaseID, string(payload), now, now)
	if err != nil {
		return fmt.Errorf("upsert database user grant: %w", err)
	}
	return nil
}

func (r *Repository) DeleteUserDatabaseGrant(ctx context.Context, userID, databaseID string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM database_user_grants WHERE user_id=? AND database_id=?`, userID, databaseID)
	if err != nil {
		return fmt.Errorf("delete database user grant: %w", err)
	}
	return requireAffected(result)
}

func (r *Repository) DeleteUser(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM database_accounts WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete database user metadata: %w", err)
	}
	return requireAffected(result)
}

func (r *Repository) decorateDatabaseUser(ctx context.Context, item DatabaseUser) (DatabaseUser, error) {
	grants, err := r.UserDatabaseGrants(ctx, item.ID)
	if err != nil {
		return DatabaseUser{}, err
	}
	item.Databases = grants
	if len(grants) > 0 {
		item.DatabaseID = grants[0].DatabaseID
		item.Privileges = append([]string(nil), grants[0].Privileges...)
	}
	return item, nil
}

func (r *Repository) CreateBackup(ctx context.Context, backup Backup) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO database_backups(id,database_id,file_name,status,size_bytes,created_at)
		VALUES(?,?,?,?,?,?)`, backup.ID, backup.DatabaseID, backup.FileName, backup.Status, backup.SizeBytes, backup.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert database backup: %w", err)
	}
	return nil
}

func (r *Repository) BackupByID(ctx context.Context, id string) (Backup, error) {
	var item Backup
	var errText, completed sql.NullString
	var created string
	err := r.db.QueryRowContext(ctx, `SELECT id,database_id,file_name,status,size_bytes,error,created_at,completed_at FROM database_backups WHERE id=?`, id).
		Scan(&item.ID, &item.DatabaseID, &item.FileName, &item.Status, &item.SizeBytes, &errText, &created, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return Backup{}, ErrNotFound
	}
	if err != nil {
		return Backup{}, fmt.Errorf("get database backup: %w", err)
	}
	if errText.Valid {
		item.Error = &errText.String
	}
	item.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return Backup{}, err
	}
	if completed.Valid {
		value, parseErr := parseDBTime(completed.String)
		if parseErr != nil {
			return Backup{}, parseErr
		}
		item.CompletedAt = &value
	}
	return item, nil
}

func (r *Repository) ListBackups(ctx context.Context, databaseID string) ([]Backup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM database_backups WHERE database_id=? ORDER BY created_at DESC`, databaseID)
	if err != nil {
		return nil, fmt.Errorf("list database backups: %w", err)
	}
	defer rows.Close()
	var out []Backup
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		item, err := r.BackupByID(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) CompleteBackup(ctx context.Context, id, status string, size int64, message *string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE database_backups SET status=?,size_bytes=?,error=?,completed_at=? WHERE id=?`, status, size, message, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update database backup: %w", err)
	}
	return requireAffected(result)
}

func (r *Repository) DeleteBackup(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM database_backups WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete database backup: %w", err)
	}
	return requireAffected(result)
}

type rowScanner func(dest ...any) error

func scanDatabaseAccount(scan rowScanner) (DatabaseUser, error) {
	var item DatabaseUser
	var secret sql.NullString
	var created, updated string
	if err := scan(&item.ID, &item.Engine, &item.Username, &secret, &created, &updated); err != nil {
		return DatabaseUser{}, err
	}
	if secret.Valid {
		item.SecretRef = secret.String
	}
	var err error
	item.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return DatabaseUser{}, err
	}
	item.UpdatedAt, err = parseDBTime(updated)
	if err != nil {
		return DatabaseUser{}, err
	}
	return item, nil
}

func parseDBTime(value string) (time.Time, error) {
	formats := []string{time.RFC3339Nano, "2006-01-02 15:04:05"}
	for _, format := range formats {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("parse database timestamp %q", value)
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

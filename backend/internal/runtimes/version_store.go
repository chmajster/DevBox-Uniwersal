package runtimes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	goruntime "runtime"
	"strings"
	"time"
)

const (
	InstallationInstalled   = "installed"
	InstallationInstalling  = "installing"
	InstallationBroken      = "broken"
	InstallationUnavailable = "unavailable"
	InstallationRemoving    = "removing"
	InstallationFailed      = "failed"
)

var (
	ErrInstallationNotFound = errors.New("runtime installation not found")
	ErrRuntimeConflict      = errors.New("runtime operation conflicts with current state")
	ErrRuntimeInUse         = errors.New("runtime installation is in use")
	ErrInvalidRuntime       = errors.New("invalid runtime")
	ErrNoRuntimeAssignment  = errors.New("runtime is not assigned to project")
)

type Installation struct {
	ID                string            `json:"id"`
	RuntimeType       string            `json:"runtime_type"`
	Version           string            `json:"version"`
	ExecutablePath    string            `json:"executable_path"`
	InstallationRoot  string            `json:"installation_root"`
	Architecture      string            `json:"architecture"`
	Platform          string            `json:"platform"`
	InstallationMethod string           `json:"installation_method"`
	ManagedByDevBox   bool              `json:"managed_by_devbox"`
	Source            string            `json:"source"`
	Status            string            `json:"status"`
	Tools             map[string]string `json:"tools,omitempty"`
	Error             string            `json:"error,omitempty"`
	InstalledAt       *time.Time         `json:"installed_at,omitempty"`
	LastValidatedAt   *time.Time         `json:"last_validated_at,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	UsedByProjects    []ProjectUsage     `json:"used_by_projects,omitempty"`
}

type ProjectUsage struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RuntimeDefault struct {
	RuntimeType      string    `json:"runtime_type"`
	InstallationID   string    `json:"runtime_installation_id"`
	RequestedVersion string    `json:"requested_version"`
	ResolvedVersion  string    `json:"resolved_version"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ProjectRuntimeAssignment struct {
	ProjectID         string    `json:"project_id"`
	RuntimeType       string    `json:"runtime_type"`
	InstallationID   string    `json:"runtime_installation_id"`
	RequestedVersion string    `json:"requested_version"`
	ResolvedVersion  string    `json:"resolved_version"`
	ExecutablePath   string    `json:"executable_path"`
	Status           string    `json:"status"`
	ManagedByDevBox  bool      `json:"managed_by_devbox"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ExecutionSelection struct {
	RuntimeType string
	Version     string
	Tools       map[string]string
	Managed     bool
}

type VersionRepository struct {
	db *sql.DB
}

func NewVersionRepository(db *sql.DB) *VersionRepository {
	return &VersionRepository{db: db}
}

func validRuntimeType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "php", "node", "python", "go":
		return true
	default:
		return false
	}
}

func runtimeID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func (r *VersionRepository) ListInstallations(ctx context.Context, runtimeType string) ([]Installation, error) {
	query := "SELECT id,runtime_type,version,executable_path,installation_root,architecture,platform,installation_method,managed_by_devbox,source,status,metadata_json,error_text,installed_at,last_validated_at,created_at,updated_at FROM runtime_installations"
	args := []any{}
	if runtimeType != "" {
		query += " WHERE runtime_type=?"
		args = append(args, runtimeType)
	}
	query += " ORDER BY runtime_type, managed_by_devbox DESC, version DESC, executable_path"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runtime installations: %w", err)
	}
	defer rows.Close()
	items := []Installation{}
	for rows.Next() {
		item, err := scanInstallation(rows.Scan)
		if err != nil {
			return nil, err
		}
		usage, err := r.UsageProjects(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		item.UsedByProjects = usage
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *VersionRepository) GetInstallation(ctx context.Context, id string) (Installation, error) {
	item, err := scanInstallation(r.db.QueryRowContext(ctx, "SELECT id,runtime_type,version,executable_path,installation_root,architecture,platform,installation_method,managed_by_devbox,source,status,metadata_json,error_text,installed_at,last_validated_at,created_at,updated_at FROM runtime_installations WHERE id=?", id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Installation{}, ErrInstallationNotFound
	}
	if err != nil {
		return Installation{}, fmt.Errorf("get runtime installation: %w", err)
	}
	usage, err := r.UsageProjects(ctx, item.ID)
	if err != nil {
		return Installation{}, err
	}
	item.UsedByProjects = usage
	return item, nil
}

func (r *VersionRepository) UpsertSystem(ctx context.Context, runtimeType, version, executablePath, source string, tools map[string]string) (Installation, error) {
	if !validRuntimeType(runtimeType) {
		return Installation{}, ErrInvalidRuntime
	}
	now := time.Now().UTC()
	metadata, _ := json.Marshal(map[string]any{"tools": tools})
	id := runtimeID()
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO runtime_installations(id,runtime_type,version,executable_path,installation_root,architecture,platform,installation_method,managed_by_devbox,source,status,metadata_json,installed_at,last_validated_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,0,?,'installed',?,?,?,?,?) ON CONFLICT(runtime_type,executable_path) WHERE managed_by_devbox=0 DO UPDATE SET version=excluded.version,source=excluded.source,status='installed',metadata_json=excluded.metadata_json,last_validated_at=excluded.last_validated_at,updated_at=excluded.updated_at",
		id, runtimeType, version, executablePath, "", goruntime.GOARCH, goruntime.GOOS, "system", source, string(metadata), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Installation{}, fmt.Errorf("upsert system runtime: %w", err)
	}
	return r.FindByExecutable(ctx, runtimeType, executablePath)
}

func (r *VersionRepository) FindByExecutable(ctx context.Context, runtimeType, executablePath string) (Installation, error) {
	item, err := scanInstallation(r.db.QueryRowContext(ctx, "SELECT id,runtime_type,version,executable_path,installation_root,architecture,platform,installation_method,managed_by_devbox,source,status,metadata_json,error_text,installed_at,last_validated_at,created_at,updated_at FROM runtime_installations WHERE runtime_type=? AND executable_path=? ORDER BY managed_by_devbox DESC LIMIT 1", runtimeType, executablePath).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Installation{}, ErrInstallationNotFound
	}
	return item, err
}

func (r *VersionRepository) PrepareManagedInstall(ctx context.Context, runtimeType, requestedVersion, resolvedVersion, method string) (Installation, error) {
	if !validRuntimeType(runtimeType) {
		return Installation{}, ErrInvalidRuntime
	}
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Installation{}, err
	}
	defer tx.Rollback()
	var existingID, status string
	err = tx.QueryRowContext(ctx, "SELECT id,status FROM runtime_installations WHERE runtime_type=? AND version=? AND platform=? AND architecture=? AND managed_by_devbox=1", runtimeType, resolvedVersion, goruntime.GOOS, goruntime.GOARCH).Scan(&existingID, &status)
	switch {
	case err == nil:
		if status == InstallationInstalling || status == InstallationRemoving || status == InstallationInstalled {
			return Installation{}, fmt.Errorf("%w: %s %s is already %s", ErrRuntimeConflict, runtimeType, resolvedVersion, status)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE runtime_installations SET status='installing',installation_method=?,error_text=NULL,source=?,updated_at=? WHERE id=?", method, requestedVersion, now.Format(time.RFC3339Nano), existingID); err != nil {
			return Installation{}, err
		}
	case errors.Is(err, sql.ErrNoRows):
		existingID = runtimeID()
		if _, err := tx.ExecContext(ctx, "INSERT INTO runtime_installations(id,runtime_type,version,executable_path,installation_root,architecture,platform,installation_method,managed_by_devbox,source,status,metadata_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,?,'installing','{}',?,?)",
			existingID, runtimeType, resolvedVersion, "", "", goruntime.GOARCH, goruntime.GOOS, method, requestedVersion, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return Installation{}, fmt.Errorf("prepare managed runtime: %w", err)
		}
	default:
		return Installation{}, err
	}
	if err := tx.Commit(); err != nil {
		return Installation{}, err
	}
	return r.GetInstallation(ctx, existingID)
}

func (r *VersionRepository) MarkInstalled(ctx context.Context, id, executablePath, installationRoot string, tools map[string]string) error {
	now := time.Now().UTC()
	metadata, _ := json.Marshal(map[string]any{"tools": tools})
	res, err := r.db.ExecContext(ctx, "UPDATE runtime_installations SET executable_path=?,installation_root=?,status='installed',metadata_json=?,error_text=NULL,installed_at=COALESCE(installed_at,?),last_validated_at=?,updated_at=? WHERE id=? AND status='installing'",
		executablePath, installationRoot, string(metadata), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: installation is no longer installing", ErrRuntimeConflict)
	}
	return nil
}

func (r *VersionRepository) MarkValidation(ctx context.Context, id, status, errorText string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, "UPDATE runtime_installations SET status=?,error_text=?,last_validated_at=?,updated_at=? WHERE id=?",
		status, nullableRuntime(errorText), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id)
	return err
}

func (r *VersionRepository) MarkFailed(ctx context.Context, id, message string) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, "UPDATE runtime_installations SET status='failed',error_text=?,updated_at=? WHERE id=?", message, now.Format(time.RFC3339Nano), id)
	return err
}

func (r *VersionRepository) MarkRemoving(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, "UPDATE runtime_installations SET status='removing',updated_at=? WHERE id=? AND managed_by_devbox=1 AND status IN ('installed','broken','failed','unavailable')", time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: runtime cannot be removed in its current state", ErrRuntimeConflict)
	}
	return nil
}

func (r *VersionRepository) DeleteInstallation(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM runtime_installations WHERE id=?", id)
	return err
}

func (r *VersionRepository) UsageProjects(ctx context.Context, installationID string) ([]ProjectUsage, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT p.id,p.name FROM project_runtime_assignments a JOIN projects p ON p.id=a.project_id WHERE a.runtime_installation_id=? ORDER BY p.name COLLATE NOCASE", installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProjectUsage{}
	for rows.Next() {
		var item ProjectUsage
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *VersionRepository) SetDefault(ctx context.Context, runtimeType, installationID, requestedVersion string, actor *string) error {
	installation, err := r.GetInstallation(ctx, installationID)
	if err != nil {
		return err
	}
	if installation.RuntimeType != runtimeType || installation.Status != InstallationInstalled {
		return fmt.Errorf("%w: default must reference an installed %s runtime", ErrInvalidRuntime, runtimeType)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = r.db.ExecContext(ctx, "INSERT INTO runtime_defaults(runtime_type,runtime_installation_id,requested_version,updated_by,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(runtime_type) DO UPDATE SET runtime_installation_id=excluded.runtime_installation_id,requested_version=excluded.requested_version,updated_by=excluded.updated_by,updated_at=excluded.updated_at",
		runtimeType, installationID, requestedVersion, actor, now)
	return err
}

func (r *VersionRepository) Defaults(ctx context.Context) ([]RuntimeDefault, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT d.runtime_type,d.runtime_installation_id,d.requested_version,i.version,d.updated_at FROM runtime_defaults d JOIN runtime_installations i ON i.id=d.runtime_installation_id ORDER BY d.runtime_type")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RuntimeDefault{}
	for rows.Next() {
		var item RuntimeDefault
		var updated string
		if err := rows.Scan(&item.RuntimeType, &item.InstallationID, &item.RequestedVersion, &item.ResolvedVersion, &updated); err != nil {
			return nil, err
		}
		item.UpdatedAt, err = parseRuntimeTime(updated)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *VersionRepository) SetAssignment(ctx context.Context, projectID, runtimeType, installationID, requestedVersion string) (ProjectRuntimeAssignment, error) {
	installation, err := r.GetInstallation(ctx, installationID)
	if err != nil {
		return ProjectRuntimeAssignment{}, err
	}
	if installation.RuntimeType != runtimeType || installation.Status != InstallationInstalled {
		return ProjectRuntimeAssignment{}, fmt.Errorf("%w: selected runtime is not installed and valid", ErrInvalidRuntime)
	}
	var exists int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id=?", projectID).Scan(&exists); err != nil {
		return ProjectRuntimeAssignment{}, err
	}
	if exists == 0 {
		return ProjectRuntimeAssignment{}, ErrProjectNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = r.db.ExecContext(ctx, "INSERT INTO project_runtime_assignments(project_id,runtime_type,runtime_installation_id,requested_version,resolved_version,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(project_id,runtime_type) DO UPDATE SET runtime_installation_id=excluded.runtime_installation_id,requested_version=excluded.requested_version,resolved_version=excluded.resolved_version,updated_at=excluded.updated_at",
		projectID, runtimeType, installationID, requestedVersion, installation.Version, now, now)
	if err != nil {
		return ProjectRuntimeAssignment{}, err
	}
	return r.Assignment(ctx, projectID, runtimeType)
}

func (r *VersionRepository) ClearAssignment(ctx context.Context, projectID, runtimeType string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM project_runtime_assignments WHERE project_id=? AND runtime_type=?", projectID, runtimeType)
	return err
}

func (r *VersionRepository) Assignment(ctx context.Context, projectID, runtimeType string) (ProjectRuntimeAssignment, error) {
	var item ProjectRuntimeAssignment
	var managed int
	var created, updated string
	err := r.db.QueryRowContext(ctx, "SELECT a.project_id,a.runtime_type,a.runtime_installation_id,a.requested_version,a.resolved_version,i.executable_path,i.status,i.managed_by_devbox,a.created_at,a.updated_at FROM project_runtime_assignments a JOIN runtime_installations i ON i.id=a.runtime_installation_id WHERE a.project_id=? AND a.runtime_type=?", projectID, runtimeType).
		Scan(&item.ProjectID, &item.RuntimeType, &item.InstallationID, &item.RequestedVersion, &item.ResolvedVersion, &item.ExecutablePath, &item.Status, &managed, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectRuntimeAssignment{}, ErrNoRuntimeAssignment
	}
	if err != nil {
		return ProjectRuntimeAssignment{}, err
	}
	item.ManagedByDevBox = managed != 0
	item.CreatedAt, err = parseRuntimeTime(created)
	if err != nil {
		return ProjectRuntimeAssignment{}, err
	}
	item.UpdatedAt, err = parseRuntimeTime(updated)
	return item, err
}

func (r *VersionRepository) Assignments(ctx context.Context, projectID string) ([]ProjectRuntimeAssignment, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT a.project_id,a.runtime_type,a.runtime_installation_id,a.requested_version,a.resolved_version,i.executable_path,i.status,i.managed_by_devbox,a.created_at,a.updated_at FROM project_runtime_assignments a JOIN runtime_installations i ON i.id=a.runtime_installation_id WHERE a.project_id=? ORDER BY a.runtime_type", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProjectRuntimeAssignment{}
	for rows.Next() {
		var item ProjectRuntimeAssignment
		var managed int
		var created, updated string
		if err := rows.Scan(&item.ProjectID, &item.RuntimeType, &item.InstallationID, &item.RequestedVersion, &item.ResolvedVersion, &item.ExecutablePath, &item.Status, &managed, &created, &updated); err != nil {
			return nil, err
		}
		item.ManagedByDevBox = managed != 0
		item.CreatedAt, err = parseRuntimeTime(created)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = parseRuntimeTime(updated)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *VersionRepository) Execution(ctx context.Context, projectID, runtimeType string) (ExecutionSelection, error) {
	assignment, err := r.Assignment(ctx, projectID, runtimeType)
	if err != nil {
		return ExecutionSelection{}, err
	}
	installation, err := r.GetInstallation(ctx, assignment.InstallationID)
	if err != nil {
		return ExecutionSelection{}, err
	}
	if installation.Status != InstallationInstalled {
		return ExecutionSelection{}, fmt.Errorf("%w: assigned runtime status is %s", ErrRuntimeConflict, installation.Status)
	}
	tools := map[string]string{}
	for key, value := range installation.Tools {
		tools[key] = value
	}
	tools[runtimeType] = installation.ExecutablePath
	return ExecutionSelection{RuntimeType: runtimeType, Version: installation.Version, Tools: tools, Managed: installation.ManagedByDevBox}, nil
}

type rowScanner func(dest ...any) error

func scanInstallation(scan rowScanner) (Installation, error) {
	var item Installation
	var managed int
	var metadata string
	var errorText, installedAt, validatedAt sql.NullString
	var created, updated string
	if err := scan(&item.ID, &item.RuntimeType, &item.Version, &item.ExecutablePath, &item.InstallationRoot, &item.Architecture, &item.Platform, &item.InstallationMethod, &managed, &item.Source, &item.Status, &metadata, &errorText, &installedAt, &validatedAt, &created, &updated); err != nil {
		return Installation{}, err
	}
	item.ManagedByDevBox = managed != 0
	if errorText.Valid {
		item.Error = errorText.String
	}
	var decoded struct {
		Tools map[string]string `json:"tools"`
	}
	_ = json.Unmarshal([]byte(metadata), &decoded)
	item.Tools = decoded.Tools
	var err error
	item.CreatedAt, err = parseRuntimeTime(created)
	if err != nil {
		return Installation{}, err
	}
	item.UpdatedAt, err = parseRuntimeTime(updated)
	if err != nil {
		return Installation{}, err
	}
	if installedAt.Valid {
		value, parseErr := parseRuntimeTime(installedAt.String)
		if parseErr != nil {
			return Installation{}, parseErr
		}
		item.InstalledAt = &value
	}
	if validatedAt.Valid {
		value, parseErr := parseRuntimeTime(validatedAt.String)
		if parseErr != nil {
			return Installation{}, parseErr
		}
		item.LastValidatedAt = &value
	}
	return item, nil
}

func parseRuntimeTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid runtime timestamp %q", value)
}

func nullableRuntime(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

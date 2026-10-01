package projects

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("project not found")

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func NewID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func (r *Repository) Create(ctx context.Context, p Project, credentialName string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,slug,description,status,work_dir,created_by,created_at,updated_at,source_type,local_path,runtime,runtime_version,container_policy,deployment_mode,working_directory,build_command,start_command,healthcheck,auto_start,current_commit) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Slug, nullable(p.Description), p.Status, p.LocalPath, p.CreatedBy, p.CreatedAt.UTC().Format(time.RFC3339Nano), p.UpdatedAt.UTC().Format(time.RFC3339Nano), p.SourceType, nullable(p.LocalPath), p.Runtime, p.RuntimeVersion, p.ContainerPolicy, "docker", p.WorkingDirectory, p.BuildCommand, p.StartCommand, p.Healthcheck, boolInt(p.AutoStart), nullable(p.CurrentCommit))
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_sources(id,project_id,provider,repository_url,reference,credential_secret_id,credential_kind,credential_name,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		NewID(), p.ID, p.SourceType, p.RepositoryURL, nullable(p.Branch), nullable(p.CredentialID), nullable(p.CredentialKind), nullable(credentialName), p.CreatedAt.UTC().Format(time.RFC3339Nano), p.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create project source: %w", err)
	}
	return tx.Commit()
}

func (r *Repository) List(ctx context.Context, includeArchived bool) ([]Project, error) {
	query := projectSelect + ` WHERE (? = 1 OR p.archived_at IS NULL) ORDER BY p.created_at DESC`
	rows, err := r.db.QueryContext(ctx, query, boolInt(includeArchived))
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (Project, error) {
	p, err := scanProject(r.db.QueryRowContext(ctx, projectSelect+` WHERE p.id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (r *Repository) ReleaseArchivedIdentity(ctx context.Context, name, slug string) error {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,slug FROM projects WHERE archived_at IS NOT NULL AND (name=? OR slug=?)`, name, slug)
	if err != nil {
		return fmt.Errorf("find archived project identity: %w", err)
	}
	defer rows.Close()
	type archivedIdentity struct{ id, name, slug string }
	var matches []archivedIdentity
	for rows.Next() {
		var item archivedIdentity
		if err := rows.Scan(&item.id, &item.name, &item.slug); err != nil {
			return err
		}
		matches = append(matches, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range matches {
		suffix := item.id
		if len(suffix) > 8 {
			suffix = suffix[:8]
		}
		nameSuffix := " [archived " + suffix + "]"
		baseName := item.name
		if maxBase := 120 - len(nameSuffix); len(baseName) > maxBase {
			baseName = baseName[:maxBase]
		}
		archivedName := baseName + nameSuffix
		archivedSlug := item.slug + "-archived-" + suffix
		if _, err := r.db.ExecContext(ctx, `UPDATE projects SET name=?,slug=?,updated_at=? WHERE id=? AND archived_at IS NOT NULL`,
			archivedName, archivedSlug, time.Now().UTC().Format(time.RFC3339Nano), item.id); err != nil {
			return fmt.Errorf("release archived project identity: %w", err)
		}
	}
	return nil
}

func (r *Repository) Update(ctx context.Context, p Project, credentialName string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE projects SET name=?,slug=?,description=?,status=?,work_dir=?,local_path=?,runtime=?,runtime_version=?,container_policy=?,working_directory=?,build_command=?,start_command=?,healthcheck=?,auto_start=?,current_commit=?,updated_at=? WHERE id=?`,
		p.Name, p.Slug, nullable(p.Description), p.Status, p.LocalPath, nullable(p.LocalPath), p.Runtime, p.RuntimeVersion, p.ContainerPolicy, p.WorkingDirectory, p.BuildCommand, p.StartCommand, p.Healthcheck, boolInt(p.AutoStart), nullable(p.CurrentCommit), p.UpdatedAt.UTC().Format(time.RFC3339Nano), p.ID)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_runtime_modules WHERE project_id=? AND runtime<>?`, p.ID, p.Runtime); err != nil {
		return fmt.Errorf("remove stale runtime modules: %w", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE project_sources SET provider=?,repository_url=?,reference=?,credential_secret_id=?,credential_kind=?,credential_name=?,updated_at=? WHERE project_id=?`,
		p.SourceType, p.RepositoryURL, nullable(p.Branch), nullable(p.CredentialID), nullable(p.CredentialKind), nullable(credentialName), p.UpdatedAt.UTC().Format(time.RFC3339Nano), p.ID)
	if err != nil {
		return fmt.Errorf("update project source: %w", err)
	}
	return tx.Commit()
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) Archive(ctx context.Context, id string, when time.Time) error {
	res, err := r.db.ExecContext(ctx, `UPDATE projects SET status='archived',archived_at=?,updated_at=? WHERE id=?`, when.UTC().Format(time.RFC3339Nano), when.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("archive project: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) UpdateGitState(ctx context.Context, id, branch, commit string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := r.db.ExecContext(ctx, `UPDATE projects SET current_commit=?,updated_at=? WHERE id=?`, nullable(commit), now, id); err != nil {
		return err
	}
	if branch != "" {
		if _, err := r.db.ExecContext(ctx, `UPDATE project_sources SET reference=?,updated_at=? WHERE project_id=?`, branch, now, id); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE projects SET status=?,updated_at=? WHERE id=?`, status, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (r *Repository) UpdateRuntime(ctx context.Context, id, runtime string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE projects SET runtime=?,updated_at=? WHERE id=?`, runtime, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (r *Repository) RuntimeContainerConfig(ctx context.Context, projectID string) (RuntimeContainerConfig, error) {
	p, err := r.Get(ctx, projectID)
	if err != nil {
		return RuntimeContainerConfig{}, err
	}
	config := RuntimeContainerConfig{
		ProjectID:       projectID,
		Runtime:         p.Runtime,
		RuntimeVersion:  p.RuntimeVersion,
		ContainerPolicy: p.ContainerPolicy,
		Modules:         []RuntimeModule{},
	}
	rows, err := r.db.QueryContext(ctx, `SELECT module_name,version_constraint FROM project_runtime_modules WHERE project_id=? AND enabled=1 ORDER BY module_name`, projectID)
	if err != nil {
		return RuntimeContainerConfig{}, fmt.Errorf("load runtime modules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var module RuntimeModule
		if err := rows.Scan(&module.Name, &module.Version); err != nil {
			return RuntimeContainerConfig{}, err
		}
		config.Modules = append(config.Modules, module)
	}
	if err := rows.Err(); err != nil {
		return RuntimeContainerConfig{}, err
	}
	var state RuntimeContainerState
	err = r.db.QueryRowContext(ctx, `SELECT container_name,image_tag,build_fingerprint FROM project_runtime_state WHERE project_id=?`, projectID).
		Scan(&state.ContainerName, &state.ImageTag, &state.Fingerprint)
	if err == nil {
		config.ContainerName = state.ContainerName
		config.ImageTag = state.ImageTag
		config.Fingerprint = state.Fingerprint
	} else if !errors.Is(err, sql.ErrNoRows) {
		return RuntimeContainerConfig{}, fmt.Errorf("load runtime state: %w", err)
	}
	return config, nil
}

func (r *Repository) SaveRuntimeContainerConfig(ctx context.Context, projectID string, config RuntimeContainerConfig) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE projects SET runtime=?,runtime_version=?,container_policy=?,updated_at=? WHERE id=?`,
		config.Runtime, config.RuntimeVersion, config.ContainerPolicy, now, projectID)
	if err != nil {
		return fmt.Errorf("update runtime container config: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_runtime_modules WHERE project_id=?`, projectID); err != nil {
		return fmt.Errorf("replace runtime modules: %w", err)
	}
	for _, module := range config.Modules {
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_runtime_modules(project_id,runtime,module_name,version_constraint,enabled,created_at,updated_at) VALUES(?,?,?,?,1,?,?)`,
			projectID, config.Runtime, module.Name, module.Version, now, now); err != nil {
			return fmt.Errorf("store runtime module %s: %w", module.Name, err)
		}
	}
	return tx.Commit()
}

func (r *Repository) RuntimeContainerState(ctx context.Context, projectID string) (RuntimeContainerState, error) {
	var state RuntimeContainerState
	err := r.db.QueryRowContext(ctx, `SELECT container_name,image_tag,build_fingerprint FROM project_runtime_state WHERE project_id=?`, projectID).
		Scan(&state.ContainerName, &state.ImageTag, &state.Fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return RuntimeContainerState{}, nil
	}
	if err != nil {
		return RuntimeContainerState{}, fmt.Errorf("load runtime state: %w", err)
	}
	return state, nil
}

func (r *Repository) SaveRuntimeContainerState(ctx context.Context, projectID string, state RuntimeContainerState) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.db.ExecContext(ctx, `INSERT INTO project_runtime_state(project_id,container_name,image_tag,build_fingerprint,updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET container_name=excluded.container_name,image_tag=excluded.image_tag,build_fingerprint=excluded.build_fingerprint,updated_at=excluded.updated_at`,
		projectID, state.ContainerName, state.ImageTag, state.Fingerprint, now)
	if err != nil {
		return fmt.Errorf("store runtime state: %w", err)
	}
	return nil
}

func (r *Repository) CentralCredentialKind(ctx context.Context, id string) (string, error) {
	var kind string
	err := r.db.QueryRowContext(ctx, `SELECT kind FROM credentials WHERE id=?`, id).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: central credential not found", ErrInvalidInput)
	}
	if err != nil {
		return "", fmt.Errorf("load central credential: %w", err)
	}
	return kind, nil
}

func (r *Repository) HasActiveDeploymentJob(ctx context.Context, projectID string) (bool, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM jobs
		WHERE project_id = ? AND type = ? AND status IN ('queued','running')
	`, projectID, JobDeploy).Scan(&count); err != nil {
		return false, fmt.Errorf("check active deployment job: %w", err)
	}
	return count > 0, nil
}

func (r *Repository) CreateDeployment(ctx context.Context, d Deployment) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO deployments(id,project_id,revision,status,runtime,metadata_json,created_by,created_at,commit_before,commit_after,duration_ms,current_stage,error_text,job_id) VALUES(?,?,?,?,?,'{}',?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, nil, d.Status, nil, d.TriggeredBy, d.CreatedAt.UTC().Format(time.RFC3339Nano), nullable(d.CommitBefore), nullable(d.CommitAfter), d.DurationMS, d.Stage, nullable(d.Error), d.JobID)
	if err != nil {
		return fmt.Errorf("create deployment: %w", err)
	}
	return nil
}

func (r *Repository) BindDeploymentJob(ctx context.Context, deploymentID, jobID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE deployments SET job_id=? WHERE id=?`, jobID, deploymentID)
	return err
}

func (r *Repository) StartDeployment(ctx context.Context, id, stage, commitBefore string, started time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE deployments SET status=?,current_stage=?,commit_before=?,started_at=?,finished_at=NULL,error_text=NULL WHERE id=?`, stage, stage, nullable(commitBefore), started.UTC().Format(time.RFC3339Nano), id)
	return err
}

func (r *Repository) SetDeploymentStage(ctx context.Context, id, stage string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE deployments SET status=?,current_stage=? WHERE id=?`, stage, stage, id)
	return err
}
func (r *Repository) SetDeploymentWaiting(ctx context.Context, id, stage, message string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE deployments SET status=?,current_stage=?,error_text=?,finished_at=NULL WHERE id=?`,
		DeploymentWaitingForConfiguration, stage, nullable(message), id)
	return err
}

func (r *Repository) WaitingDeployment(ctx context.Context, projectID string) (Deployment, bool, error) {
	items, err := r.ListDeployments(ctx, projectID)
	if err != nil {
		return Deployment{}, false, err
	}
	for _, item := range items {
		if item.Status == DeploymentWaitingForConfiguration && item.FinishedAt == nil {
			return item, true, nil
		}
	}
	return Deployment{}, false, nil
}


func (r *Repository) FinishDeployment(ctx context.Context, id, status, stage, commitAfter, errorText string, finished time.Time, duration time.Duration) error {
	_, err := r.db.ExecContext(ctx, `UPDATE deployments SET status=?,current_stage=?,commit_after=?,error_text=?,finished_at=?,duration_ms=?,revision=? WHERE id=?`,
		status, stage, nullable(commitAfter), nullable(errorText), finished.UTC().Format(time.RFC3339Nano), duration.Milliseconds(), nullable(commitAfter), id)
	return err
}

func (r *Repository) ListDeployments(ctx context.Context, projectID string) ([]Deployment, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,project_id,job_id,commit_before,commit_after,started_at,finished_at,duration_ms,status,current_stage,error_text,created_by,created_at FROM deployments WHERE project_id=? ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deployment
	for rows.Next() {
		d, err := scanDeployment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

const projectSelect = `SELECT p.id,p.name,p.slug,COALESCE(p.description,''),p.status,p.source_type,COALESCE(s.repository_url,''),COALESCE(s.reference,''),COALESCE(p.local_path,''),p.runtime,p.runtime_version,p.container_policy,p.working_directory,p.build_command,p.start_command,p.healthcheck,p.auto_start,COALESCE(s.credential_kind,''),COALESCE(s.credential_secret_id,''),COALESCE(p.current_commit,''),p.created_by,p.created_at,p.updated_at,p.archived_at,(SELECT port FROM ports WHERE project_id=p.id AND released_at IS NULL AND state<>'released' AND purpose<>'application-https' ORDER BY (purpose='application') DESC,created_at DESC LIMIT 1),(SELECT hostname FROM domains WHERE project_id=p.id ORDER BY created_at DESC LIMIT 1) FROM projects p LEFT JOIN project_sources s ON s.project_id=p.id`

type scanFunc func(dest ...any) error

func scanProject(scan scanFunc) (Project, error) {
	var p Project
	var auto int
	var createdBy, archived, domain sql.NullString
	var port sql.NullInt64
	var created, updated string
	if err := scan(&p.ID, &p.Name, &p.Slug, &p.Description, &p.Status, &p.SourceType, &p.RepositoryURL, &p.Branch, &p.LocalPath, &p.Runtime, &p.RuntimeVersion, &p.ContainerPolicy, &p.WorkingDirectory, &p.BuildCommand, &p.StartCommand, &p.Healthcheck, &auto, &p.CredentialKind, &p.CredentialID, &p.CurrentCommit, &createdBy, &created, &updated, &archived, &port, &domain); err != nil {
		return Project{}, err
	}
	p.AutoStart = auto != 0
	if createdBy.Valid {
		p.CreatedBy = &createdBy.String
	}
	var err error
	p.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return Project{}, err
	}
	p.UpdatedAt, err = parseDBTime(updated)
	if err != nil {
		return Project{}, err
	}
	if archived.Valid && archived.String != "" {
		t, err := parseDBTime(archived.String)
		if err != nil {
			return Project{}, err
		}
		p.ArchivedAt = &t
	}
	if port.Valid {
		v := int(port.Int64)
		p.Port = &v
	}
	if domain.Valid {
		p.Domain = &domain.String
	}
	return p, nil
}

func scanDeployment(scan scanFunc) (Deployment, error) {
	var d Deployment
	var jobID, before, after, started, finished, errText, actor sql.NullString
	var created string
	if err := scan(&d.ID, &d.ProjectID, &jobID, &before, &after, &started, &finished, &d.DurationMS, &d.Status, &d.Stage, &errText, &actor, &created); err != nil {
		return Deployment{}, err
	}
	if jobID.Valid {
		d.JobID = &jobID.String
	}
	if before.Valid {
		d.CommitBefore = before.String
	}
	if after.Valid {
		d.CommitAfter = after.String
	}
	if errText.Valid {
		d.Error = errText.String
	}
	if actor.Valid {
		d.TriggeredBy = &actor.String
	}
	var err error
	d.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return Deployment{}, err
	}
	if started.Valid {
		t, e := parseDBTime(started.String)
		if e != nil {
			return Deployment{}, e
		}
		d.StartedAt = &t
	}
	if finished.Valid {
		t, e := parseDBTime(finished.String)
		if e != nil {
			return Deployment{}, e
		}
		d.FinishedAt = &t
	}
	return d, nil
}

func parseDBTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse database time %q", value)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

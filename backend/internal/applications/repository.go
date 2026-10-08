package applications

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// errSlugConflict distinguishes generated identity collisions from duplicate display names.
var errSlugConflict = fmt.Errorf("%w: generated application identifier already exists", ErrConflict)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func NewID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func dbTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseDBTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported database timestamp %q", value)
}

func (r *Repository) Create(ctx context.Context, app Application, source Source) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sourceJSON, environment := separateEnvironment(app.SourceConfig)
	_, err = tx.ExecContext(ctx, `INSERT INTO applications(
		id,name,slug,description,source_type,source_config_json,driver,desired_state,observed_state,health_state,auto_start,created_by,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		app.ID, app.Name, app.Slug, app.Description, app.SourceType, string(sourceJSON), app.Driver,
		app.DesiredState, app.ObservedState, app.HealthState, boolInt(app.AutoStart), app.CreatedBy, dbTime(app.CreatedAt), dbTime(app.UpdatedAt))
	if err != nil {
		return classifyDBError("create application", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO application_sources(
		application_id,repository_url,reference,local_path,credential_secret_id,current_revision,created_at,updated_at,docker_image
	) VALUES(?,?,?,?,?,?,?,?,?)`,
		app.ID, nullText(source.RepositoryURL), nullText(source.Reference), nullText(source.LocalPath),
		source.CredentialID, nullText(source.CurrentRevision), dbTime(app.CreatedAt), dbTime(app.UpdatedAt), nullText(source.DockerImage))
	if err != nil {
		return fmt.Errorf("create application source: %w", err)
	}
	if err := saveEnvironment(ctx, tx, app.ID, environment); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) List(ctx context.Context) ([]Application, error) {
	rows, err := r.db.QueryContext(ctx, applicationSelect+` ORDER BY a.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	defer rows.Close()
	out := []Application{}
	for rows.Next() {
		item, err := scanApplication(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (Application, error) {
	item, err := scanApplication(r.db.QueryRowContext(ctx, applicationSelect+` WHERE a.id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Application{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) Source(ctx context.Context, id string) (Source, error) {
	var source Source
	var repositoryURL, reference, localPath, credentialID, revision, image sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT application_id,repository_url,reference,local_path,credential_secret_id,current_revision,docker_image
		FROM application_sources WHERE application_id=?`, id).Scan(
		&source.ApplicationID, &repositoryURL, &reference, &localPath, &credentialID, &revision, &image)
	if errors.Is(err, sql.ErrNoRows) {
		return Source{}, ErrNotFound
	}
	if err != nil {
		return Source{}, err
	}
	source.RepositoryURL, source.Reference, source.LocalPath = repositoryURL.String, reference.String, localPath.String
	source.CurrentRevision = revision.String
	source.DockerImage = image.String
	if credentialID.Valid {
		value := credentialID.String
		source.CredentialID = &value
	}
	return source, nil
}

func (r *Repository) Update(ctx context.Context, app Application) error {
	sourceJSON, environment := separateEnvironment(app.SourceConfig)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE applications SET name=?,slug=?,description=?,source_config_json=?,driver=?,desired_state=?,observed_state=?,health_state=?,auto_start=?,updated_at=? WHERE id=?`,
		app.Name, app.Slug, app.Description, string(sourceJSON), app.Driver, app.DesiredState, app.ObservedState, app.HealthState, boolInt(app.AutoStart), dbTime(app.UpdatedAt), app.ID)
	if err != nil {
		return classifyDBError("update application", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := saveEnvironment(ctx, tx, app.ID, environment); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) UpdateSource(ctx context.Context, source Source) error {
	res, err := r.db.ExecContext(ctx, `UPDATE application_sources SET repository_url=?,reference=?,local_path=?,credential_secret_id=?,current_revision=?,updated_at=? WHERE application_id=?`,
		nullText(source.RepositoryURL), nullText(source.Reference), nullText(source.LocalPath), source.CredentialID,
		nullText(source.CurrentRevision), dbTime(time.Now()), source.ApplicationID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM applications WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SaveRuntime(ctx context.Context, runtime Runtime) error {
	metadata, err := json.Marshal(runtime.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO application_runtime(application_id,runtime,version,adapter_metadata_json,updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(application_id) DO UPDATE SET runtime=excluded.runtime,version=excluded.version,adapter_metadata_json=excluded.adapter_metadata_json,updated_at=excluded.updated_at`,
		runtime.ApplicationID, runtime.Name, runtime.Version, string(metadata), dbTime(time.Now()))
	return err
}

func (r *Repository) DeleteRuntime(ctx context.Context, applicationID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM application_runtime WHERE application_id=?`, applicationID)
	return err
}

func (r *Repository) Runtime(ctx context.Context, id string) (*Runtime, error) {
	var item Runtime
	var metadata string
	err := r.db.QueryRowContext(ctx, `SELECT application_id,runtime,version,adapter_metadata_json FROM application_runtime WHERE application_id=?`, id).
		Scan(&item.ApplicationID, &item.Name, &item.Version, &metadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.Metadata = map[string]any{}
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	return &item, nil
}

func (r *Repository) ReplaceTopology(ctx context.Context, applicationID string, workloads []Workload, endpoints []Endpoint) error {
	return r.storeTopology(ctx, applicationID, workloads, endpoints, true)
}

// StageTopology keeps previously managed resources until external deployment
// succeeds. Failed Compose runs may leave a mixture that must remain visible.
func (r *Repository) StageTopology(ctx context.Context, applicationID string, workloads []Workload, endpoints []Endpoint) error {
	return r.storeTopology(ctx, applicationID, workloads, endpoints, false)
}
func (r *Repository) storeTopology(ctx context.Context, applicationID string, workloads []Workload, endpoints []Endpoint, prune bool) error {

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	existingWorkloads := map[string]Workload{}
	rows, err := tx.QueryContext(ctx, `SELECT id,name,COALESCE(driver_resource_id,''),COALESCE(image,''),desired_state,observed_state,health_state,primary_workload,metadata_json,created_at,updated_at FROM workloads WHERE application_id=?`, applicationID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item Workload
		var primary int
		var metadata, created, updated string
		if err := rows.Scan(&item.ID, &item.Name, &item.DriverResourceID, &item.Image, &item.DesiredState, &item.ObservedState, &item.HealthState, &primary, &metadata, &created, &updated); err != nil {
			rows.Close()
			return err
		}
		item.ApplicationID, item.Primary = applicationID, primary != 0
		item.Metadata = map[string]any{}
		_ = json.Unmarshal([]byte(metadata), &item.Metadata)
		item.CreatedAt, _ = parseDBTime(created)
		item.UpdatedAt, _ = parseDBTime(updated)
		existingWorkloads[item.Name] = item
	}
	if err := rows.Close(); err != nil {
		return err
	}

	existingEndpoints := map[string]Endpoint{}
	rows, err = tx.QueryContext(ctx, `SELECT id,workload_id,name,protocol,container_port,host_port,domain,public,primary_endpoint,tls_mode,COALESCE(health_path,''),status,created_at,updated_at FROM endpoints WHERE application_id=?`, applicationID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item Endpoint
		var hostPort sql.NullInt64
		var domain sql.NullString
		var public, primary int
		var created, updated string
		if err := rows.Scan(&item.ID, &item.WorkloadID, &item.Name, &item.Protocol, &item.ContainerPort, &hostPort, &domain, &public, &primary, &item.TLSMode, &item.HealthPath, &item.Status, &created, &updated); err != nil {
			rows.Close()
			return err
		}
		item.ApplicationID = applicationID
		if hostPort.Valid {
			value := int(hostPort.Int64)
			item.HostPort = &value
		}
		if domain.Valid {
			value := domain.String
			item.Domain = &value
		}
		item.Public, item.Primary = public != 0, primary != 0
		item.CreatedAt, _ = parseDBTime(created)
		item.UpdatedAt, _ = parseDBTime(updated)
		existingEndpoints[item.Name] = item
	}
	if err := rows.Close(); err != nil {
		return err
	}

	now := time.Now().UTC()
	nameIDs := map[string]string{}
	idRemap := map[string]string{}
	plannedWorkloads := map[string]bool{}
	for i := range workloads {
		item := &workloads[i]
		originalID := item.ID
		if old, ok := existingWorkloads[item.Name]; ok {
			item.ID = old.ID
			item.DriverResourceID = old.DriverResourceID
			if item.Image == "" {
				item.Image = old.Image
			}
			if item.ObservedState == "" || item.ObservedState == ObservedUnknown {
				item.ObservedState = old.ObservedState
			}
			if item.HealthState == "" || item.HealthState == HealthUnknown {
				item.HealthState = old.HealthState
			}
			item.CreatedAt = old.CreatedAt
		}
		if item.ID == "" {
			item.ID = NewID()
		}
		if originalID != "" {
			idRemap[originalID] = item.ID
		}
		item.ApplicationID = applicationID
		if item.DesiredState == "" {
			item.DesiredState = DesiredRunning
		}
		if item.ObservedState == "" {
			item.ObservedState = ObservedUnknown
		}
		if item.HealthState == "" {
			item.HealthState = HealthUnknown
		}
		if item.Role == "" {
			item.Role = "internal"
		}
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		item.UpdatedAt = now
		metadata, _ := json.Marshal(item.Metadata)
		_, err := tx.ExecContext(ctx, `INSERT INTO workloads(id,application_id,name,role,driver_resource_id,image,desired_state,observed_state,health_state,primary_workload,metadata_json,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name,role=excluded.role,driver_resource_id=excluded.driver_resource_id,image=excluded.image,desired_state=excluded.desired_state,observed_state=excluded.observed_state,health_state=excluded.health_state,primary_workload=excluded.primary_workload,metadata_json=excluded.metadata_json,updated_at=excluded.updated_at`,
			item.ID, applicationID, item.Name, item.Role, nullText(item.DriverResourceID), nullText(item.Image), item.DesiredState, item.ObservedState, item.HealthState, boolInt(item.Primary), string(metadata), dbTime(item.CreatedAt), dbTime(item.UpdatedAt))
		if err != nil {
			return fmt.Errorf("store workload %s: %w", item.Name, err)
		}
		nameIDs[item.Name] = item.ID
		plannedWorkloads[item.Name] = true
	}

	plannedEndpoints := map[string]bool{}
	for i := range endpoints {
		item := &endpoints[i]
		originalWorkloadID := item.WorkloadID
		if mapped := idRemap[originalWorkloadID]; mapped != "" {
			item.WorkloadID = mapped
		}
		if old, ok := existingEndpoints[item.Name]; ok {
			item.ID = old.ID
			if item.HostPort == nil {
				item.HostPort = old.HostPort
			}
			if item.Domain == nil {
				item.Domain = old.Domain
			}
			if item.Status == "" || item.Status == ObservedUnknown {
				item.Status = old.Status
			}
			item.CreatedAt = old.CreatedAt
		}
		if item.ID == "" {
			item.ID = NewID()
		}
		item.ApplicationID = applicationID
		if item.WorkloadID == "" {
			if id := nameIDs[item.Name]; id != "" {
				item.WorkloadID = id
			}
			if item.WorkloadID == "" && len(workloads) == 1 {
				item.WorkloadID = workloads[0].ID
			}
		}
		if item.WorkloadID == "" {
			return fmt.Errorf("%w: endpoint %s does not reference a workload", ErrInvalidInput, item.Name)
		}
		if item.Protocol == "" {
			item.Protocol = "http"
		}
		if item.TLSMode == "" {
			item.TLSMode = "none"
		}
		if item.Status == "" {
			item.Status = ObservedUnknown
		}
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		item.UpdatedAt = now
		_, err := tx.ExecContext(ctx, `INSERT INTO endpoints(id,application_id,workload_id,name,protocol,container_port,host_port,domain,public,primary_endpoint,tls_mode,health_path,status,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET workload_id=excluded.workload_id,name=excluded.name,protocol=excluded.protocol,container_port=excluded.container_port,host_port=excluded.host_port,domain=excluded.domain,public=excluded.public,primary_endpoint=excluded.primary_endpoint,tls_mode=excluded.tls_mode,health_path=excluded.health_path,status=excluded.status,updated_at=excluded.updated_at`,
			item.ID, applicationID, item.WorkloadID, item.Name, item.Protocol, item.ContainerPort, item.HostPort, item.Domain, boolInt(item.Public), boolInt(item.Primary), item.TLSMode, nullText(item.HealthPath), item.Status, dbTime(item.CreatedAt), dbTime(item.UpdatedAt))
		if err != nil {
			return fmt.Errorf("store endpoint %s: %w", item.Name, err)
		}
		plannedEndpoints[item.Name] = true
	}

	if !prune {
		return tx.Commit()
	}
	for name, old := range existingEndpoints {
		if plannedEndpoints[name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM application_port_leases WHERE endpoint_id=?`, old.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM endpoints WHERE id=?`, old.ID); err != nil {
			return err
		}
	}
	for name, old := range existingWorkloads {
		if plannedWorkloads[name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM workloads WHERE id=?`, old.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) Workloads(ctx context.Context, applicationID string) ([]Workload, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,application_id,name,role,COALESCE(driver_resource_id,''),COALESCE(image,''),desired_state,observed_state,health_state,primary_workload,metadata_json,created_at,updated_at FROM workloads WHERE application_id=? ORDER BY primary_workload DESC,name`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workload{}
	for rows.Next() {
		var item Workload
		var primary int
		var metadata, created, updated string
		if err := rows.Scan(&item.ID, &item.ApplicationID, &item.Name, &item.Role, &item.DriverResourceID, &item.Image, &item.DesiredState, &item.ObservedState, &item.HealthState, &primary, &metadata, &created, &updated); err != nil {
			return nil, err
		}
		item.Primary = primary != 0
		item.Metadata = map[string]any{}
		_ = json.Unmarshal([]byte(metadata), &item.Metadata)
		item.CreatedAt, _ = parseDBTime(created)
		item.UpdatedAt, _ = parseDBTime(updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) Endpoints(ctx context.Context, applicationID string) ([]Endpoint, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,application_id,workload_id,name,protocol,container_port,host_port,domain,public,primary_endpoint,tls_mode,COALESCE(health_path,''),status,created_at,updated_at,EXISTS(SELECT 1 FROM application_routes r WHERE r.endpoint_id=endpoints.id AND r.domain=endpoints.domain AND r.target_port=endpoints.host_port AND r.tls_mode=endpoints.tls_mode AND r.active=1) FROM endpoints WHERE application_id=? ORDER BY primary_endpoint DESC,name`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Endpoint{}
	for rows.Next() {
		var item Endpoint
		var hostPort sql.NullInt64
		var domain sql.NullString
		var public, primary int
		var created, updated string
		if err := rows.Scan(&item.ID, &item.ApplicationID, &item.WorkloadID, &item.Name, &item.Protocol, &item.ContainerPort, &hostPort, &domain, &public, &primary, &item.TLSMode, &item.HealthPath, &item.Status, &created, &updated, &item.RouteActive); err != nil {
			return nil, err
		}
		if hostPort.Valid {
			value := int(hostPort.Int64)
			item.HostPort = &value
		}
		if domain.Valid {
			value := domain.String
			item.Domain = &value
		}
		item.Public, item.Primary = public != 0, primary != 0
		item.CreatedAt, _ = parseDBTime(created)
		item.UpdatedAt, _ = parseDBTime(updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateWorkloadObserved(ctx context.Context, id, resourceID, observed, health, image string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE workloads SET driver_resource_id=?,image=?,observed_state=?,health_state=?,updated_at=? WHERE id=?`,
		nullText(resourceID), nullText(image), observed, health, dbTime(time.Now()), id)
	return err
}

func (r *Repository) UpdateEndpointRuntime(ctx context.Context, id string, hostPort *int, status string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE endpoints SET host_port=?,status=?,updated_at=? WHERE id=?`, hostPort, status, dbTime(time.Now()), id)
	return err
}

func (r *Repository) UpdateApplicationState(ctx context.Context, id, observed, health string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE applications SET observed_state=?,health_state=?,updated_at=? WHERE id=?`, observed, health, dbTime(time.Now()), id)
	return err
}

func (r *Repository) CreateDeployment(ctx context.Context, item Deployment) error {
	snapshot := item.PlanSnapshot
	if len(snapshot) == 0 {
		snapshot = []byte("{}")
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO application_deployments(id,application_id,job_id,driver,status,stage,source_revision,started_at,finished_at,triggered_by,error_text,plan_snapshot_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.ApplicationID, item.JobID, item.Driver, item.Status, item.Stage, nullText(item.SourceRevision), timePtr(item.StartedAt), timePtr(item.FinishedAt), item.TriggeredBy, nullText(item.Error), string(snapshot), dbTime(item.CreatedAt))
	return err
}

func (r *Repository) BindDeploymentJob(ctx context.Context, deploymentID, jobID string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE application_deployments SET job_id=? WHERE id=?`, jobID, deploymentID)
	return err
}

func (r *Repository) SetDeploymentStage(ctx context.Context, id, status, stage string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE application_deployments SET status=?,stage=?,started_at=COALESCE(started_at,?),finished_at=NULL,error_text=NULL WHERE id=?`, status, stage, dbTime(time.Now()), id)
	return err
}

func (r *Repository) SaveDeploymentPlan(ctx context.Context, id string, plan DeploymentPlan) error {
	payload, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE application_deployments SET plan_snapshot_json=?,driver=?,source_revision=? WHERE id=?`, string(payload), plan.Driver, nullText(plan.SourceRevision), id)
	return err
}

func (r *Repository) FinishDeployment(ctx context.Context, id, status, stage, errorText string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE application_deployments SET status=?,stage=?,error_text=?,finished_at=? WHERE id=?`, status, stage, nullText(errorText), dbTime(time.Now()), id)
	return err
}

func (r *Repository) Deployments(ctx context.Context, applicationID string) ([]Deployment, error) {
	rows, err := r.db.QueryContext(ctx, deploymentSelect+` WHERE application_id=? ORDER BY created_at DESC`, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Deployment{}
	for rows.Next() {
		item, err := scanDeployment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) Deployment(ctx context.Context, id string) (Deployment, error) {
	item, err := scanDeployment(r.db.QueryRowContext(ctx, deploymentSelect+` WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return Deployment{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) Event(ctx context.Context, applicationID, deploymentID, workloadID, eventType, stage, message string, metadata map[string]any) error {
	payload, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO application_events(application_id,deployment_id,workload_id,type,stage,message,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		applicationID, nullText(deploymentID), nullText(workloadID), eventType, nullText(stage), nullText(message), string(payload), dbTime(time.Now()))
	return err
}

func (r *Repository) Events(ctx context.Context, applicationID string, limit int) ([]map[string]any, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,deployment_id,workload_id,type,COALESCE(stage,''),COALESCE(message,''),metadata_json,created_at FROM application_events WHERE application_id=? ORDER BY id DESC LIMIT ?`, applicationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var deploymentID, workloadID sql.NullString
		var typ, stage, message, metadata, created string
		if err := rows.Scan(&id, &deploymentID, &workloadID, &typ, &stage, &message, &metadata, &created); err != nil {
			return nil, err
		}
		meta := map[string]any{}
		_ = json.Unmarshal([]byte(metadata), &meta)
		out = append(out, map[string]any{"id": id, "deployment_id": nullStringValue(deploymentID), "workload_id": nullStringValue(workloadID), "type": typ, "stage": stage, "message": message, "metadata": meta, "created_at": created})
	}
	return out, rows.Err()
}

const applicationSelect = `SELECT a.id,a.name,a.slug,a.description,a.source_type,json_patch(a.source_config_json,COALESCE((SELECT json_object('environment',json_group_object(name,value)) FROM application_environment_variables WHERE application_id=a.id AND workload_id IS NULL HAVING COUNT(*)>0),'{}')),a.driver,a.desired_state,a.observed_state,a.health_state,a.auto_start,a.created_by,a.created_at,a.updated_at FROM applications a`
const deploymentSelect = `SELECT id,application_id,job_id,driver,status,stage,COALESCE(source_revision,''),started_at,finished_at,triggered_by,COALESCE(error_text,''),plan_snapshot_json,created_at FROM application_deployments`

type scanFn func(...any) error

func scanApplication(scan scanFn) (Application, error) {
	var item Application
	var sourceJSON, created, updated string
	var autoStart int
	var createdBy sql.NullString
	if err := scan(&item.ID, &item.Name, &item.Slug, &item.Description, &item.SourceType, &sourceJSON, &item.Driver, &item.DesiredState, &item.ObservedState, &item.HealthState, &autoStart, &createdBy, &created, &updated); err != nil {
		return Application{}, err
	}
	item.SourceConfig = json.RawMessage(sourceJSON)
	item.AutoStart = autoStart != 0
	if createdBy.Valid {
		value := createdBy.String
		item.CreatedBy = &value
	}
	var err error
	item.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return Application{}, err
	}
	item.UpdatedAt, err = parseDBTime(updated)
	return item, err
}

func scanDeployment(scan scanFn) (Deployment, error) {
	var item Deployment
	var jobID, started, finished, triggeredBy sql.NullString
	var snapshot, created string
	if err := scan(&item.ID, &item.ApplicationID, &jobID, &item.Driver, &item.Status, &item.Stage, &item.SourceRevision, &started, &finished, &triggeredBy, &item.Error, &snapshot, &created); err != nil {
		return Deployment{}, err
	}
	if jobID.Valid {
		value := jobID.String
		item.JobID = &value
	}
	if triggeredBy.Valid {
		value := triggeredBy.String
		item.TriggeredBy = &value
	}
	if started.Valid {
		value, err := parseDBTime(started.String)
		if err != nil {
			return Deployment{}, err
		}
		item.StartedAt = &value
	}
	if finished.Valid {
		value, err := parseDBTime(finished.String)
		if err != nil {
			return Deployment{}, err
		}
		item.FinishedAt = &value
	}
	item.PlanSnapshot = json.RawMessage(snapshot)
	item.CreatedAt, _ = parseDBTime(created)
	return item, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func nullText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
func nullStringValue(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
func timePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return dbTime(*value)
}

func classifyDBError(operation string, err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unique constraint") {
		if strings.Contains(message, "applications.name") {
			return fmt.Errorf("%w: an application with this name already exists; choose a different name or open the existing application", ErrConflict)
		}
		if strings.Contains(message, "applications.slug") {
			return fmt.Errorf("%w: %s", errSlugConflict, operation)
		}
		return fmt.Errorf("%w: %s", ErrConflict, operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func separateEnvironment(raw json.RawMessage) (json.RawMessage, map[string]string) {
	config := decodeConfiguration(raw)
	environment := map[string]string{}
	data, _ := json.Marshal(config["environment"])
	_ = json.Unmarshal(data, &environment)
	delete(config, "environment")
	clean, _ := json.Marshal(config)
	return clean, environment
}
func saveEnvironment(ctx context.Context, tx *sql.Tx, id string, env map[string]string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM application_environment_variables WHERE application_id=? AND workload_id IS NULL`, id); err != nil {
		return err
	}
	now := dbTime(time.Now())
	for name, value := range env {
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_environment_variables(id,application_id,name,value,created_at,updated_at) VALUES(?,?,?,?,?,?)`, NewID(), id, name, value, now, now); err != nil {
			return err
		}
	}
	return nil
}

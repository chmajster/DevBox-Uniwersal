package databases

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

const JobApplicationDatabaseProvision = "application.database-provision"
const JobApplicationDatabaseTest = "application.database-test"

func (s *Service) applicationIdle(ctx context.Context, id string) error {
	var found, active int
	if err := s.repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications WHERE id=?`, id).Scan(&found); err != nil {
		return err
	}
	if found == 0 {
		return ErrNotFound
	}
	if err := s.repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE application_id=? AND status IN ('queued','running')`, id).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return errors.New("application operation is already active")
	}
	return nil
}

func (s *Service) QueueApplicationDatabase(ctx context.Context, id, action string, payload map[string]any, actor *string) (domain.Job, error) {
	if err := s.applicationIdle(ctx, id); err != nil {
		return domain.Job{}, err
	}
	if action != JobApplicationDatabaseProvision && action != JobApplicationDatabaseTest {
		return domain.Job{}, errors.New("invalid database operation")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if err := validApplicationDatabasePayload(payload); err != nil {
		return domain.Job{}, err
	}
	return s.jobs.Enqueue(ctx, jobs.Request{Type: action, ApplicationID: &id, RequestedBy: actor, Payload: payload})
}

type applicationDatabaseJob struct {
	service *Service
	kind    string
}

func (h *applicationDatabaseJob) Type() string { return h.kind }
func (h *applicationDatabaseJob) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	if job.ApplicationID == nil {
		return nil, errors.New("application_id is required")
	}
	id := *job.ApplicationID
	stage := func(name string, progress int) {
		if logger, ok := h.service.jobs.(interface {
			Log(context.Context, string, string, string, map[string]any) error
		}); ok {
			_ = logger.Log(ctx, job.ID, "info", "application.database.stage", map[string]any{"stage": name, "progress": progress, "application_id": id})
		}
	}
	if h.kind == JobApplicationDatabaseTest {
		stage("RESOLVE_DATABASE_ACCOUNT", 15)
		connection, password, found, err := h.service.ResolveBoundApplicationDatabase(ctx, id)
		if err != nil {
			return nil, err
		}
		defer clear(password)
		if !found {
			return nil, errors.New("application has no database binding")
		}
		tester, ok := h.service.compose.(providers.ApplicationContainerDatabaseTester)
		if !ok {
			return nil, errors.New("application container SQL testing is unavailable")
		}
		workload, _ := job.Payload["workload"].(string)
		query := `SELECT COALESCE(driver_resource_id,'') FROM workloads WHERE application_id=? AND primary_workload=1`
		args := []any{id}
		if workload != "" {
			query = `SELECT COALESCE(driver_resource_id,'') FROM workloads WHERE application_id=? AND name=? AND role NOT IN ('database','cache','queue','search')`
			args = append(args, workload)
		}
		var container string
		if err := h.service.repo.db.QueryRowContext(ctx, query, args...).Scan(&container); err != nil || container == "" {
			return nil, errors.New("run the selected application workload before testing SQL")
		}
		stage("AUTHENTICATE_AND_SELECT_1", 65)
		if err := tester.TestApplicationContainerDatabase(ctx, container, connection, password); err != nil {
			return nil, err
		}
		stage("SUCCESS", 100)
		return map[string]any{"status": "success", "query": "SELECT 1", "result": 1, "container": container}, nil
	}
	engine, _ := job.Payload["engine"].(string)
	name, _ := job.Payload["name"].(string)
	username, _ := job.Payload["username"].(string)
	databaseID, _ := job.Payload["database_id"].(string)
	if name == "" && databaseID == "" {
		return nil, errors.New("database name is required")
	}
	if strings.EqualFold(username, "root") || strings.EqualFold(username, "postgres") {
		return nil, errors.New("choose an application account, not a SQL administrator")
	}
	var db Database
	var err error
	if databaseID != "" {
		db, err = h.service.repo.DatabaseByID(ctx, databaseID)
		if err != nil {
			return nil, err
		}
		engine = db.Engine
	}
	if engine == "" {
		engine = "mysql"
	}
	privileges := []string{}
	if values, ok := job.Payload["privileges"].([]any); ok {
		for _, value := range values {
			if text, ok := value.(string); ok {
				privileges = append(privileges, text)
			}
		}
	} else if values, ok := job.Payload["privileges"].([]string); ok {
		privileges = values
	}
	if len(privileges) > 0 {
		if engine == "postgresql" {
			_, err = normalizePostgreSQLPrivileges(privileges)
		} else {
			_, err = normalizePrivileges(privileges)
		}
		if err != nil {
			return nil, err
		}
	}
	stage("PREPARE_DATABASE_SERVER", 15)
	if err := h.service.ensureApplicationDatabaseServer(ctx, engine); err != nil {
		return nil, err
	}
	stage("PREPARE_DATABASE", 45)
	if databaseID == "" {
		db, err = h.service.CreateDatabase(ctx, name, engine, "", job.RequestedBy, nil)
	} else {
		db, err = h.service.repo.DatabaseByID(ctx, databaseID)
	}
	if err != nil {
		return nil, err
	}
	stage("CREATE_ACCOUNT_AND_GRANTS", 70)
	user, password, err := h.service.CreateUser(ctx, db.ID, username, nil, privileges, job.RequestedBy, nil)
	password = "" // API/job results never contain generated credentials.
	_ = password
	if err != nil {
		return nil, fmt.Errorf("database %s was created; create/select its application user before retrying: %w", db.Name, err)
	}
	stage("BIND_APPLICATION", 90)
	if err := h.service.repo.UpsertApplicationDatabaseBinding(ctx, id, db.ID, user.ID); err != nil {
		return nil, err
	}
	stage("SUCCESS", 100)
	return map[string]any{"database_id": db.ID, "user_id": user.ID, "status": "success"}, nil
}

func validApplicationDatabasePayload(payload map[string]any) error {
	for key := range payload {
		if key != "engine" && key != "name" && key != "username" && key != "privileges" && key != "database_id" && key != "workload" {
			return fmt.Errorf("unsupported database option %s", key)
		}
	}
	if engine, ok := payload["engine"].(string); ok && engine != "" && engine != "mysql" && engine != "mariadb" && engine != "postgresql" {
		return errors.New("unsupported database engine")
	}
	for _, key := range []string{"name", "username", "engine", "database_id", "workload"} {
		if value, exists := payload[key]; exists {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be a string", key)
			}
		}
	}
	for _, key := range []string{"name", "username"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			if err := ValidateIdentifier(value); err != nil {
				return err
			}
		}
	}
	return nil
}

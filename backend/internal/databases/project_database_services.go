package databases

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ProjectDatabaseServices struct {
	ProjectID string    `json:"project_id"`
	Engines   []string  `json:"engines"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type ProjectDatabaseServicesInput struct {
	Engines []string `json:"engines"`
}

func normalizeProjectDatabaseServiceEngines(values []string) ([]string, error) {
	selected := map[string]bool{}
	for _, raw := range values {
		engine := normalizeDatabaseEngineName(raw)
		if engine == "mariadb" {
			engine = "mysql"
		}
		switch engine {
		case "mysql", "postgresql":
			selected[engine] = true
		case "":
			continue
		default:
			return nil, fmt.Errorf("unsupported project database service %q", raw)
		}
	}
	result := make([]string, 0, 2)
	if selected["mysql"] {
		result = append(result, "mysql")
	}
	if selected["postgresql"] {
		result = append(result, "postgresql")
	}
	return result, nil
}

func (r *Repository) ProjectDatabaseServices(ctx context.Context, projectID string) (ProjectDatabaseServices, error) {
	var mysqlEnabled, postgresqlEnabled int
	var updated string
	err := r.db.QueryRowContext(ctx, `
		SELECT mysql_enabled, postgresql_enabled, updated_at
		FROM project_database_service_access
		WHERE project_id=?`, projectID).Scan(&mysqlEnabled, &postgresqlEnabled, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectDatabaseServices{ProjectID: projectID, Engines: []string{}}, nil
	}
	if err != nil {
		return ProjectDatabaseServices{}, fmt.Errorf("get project database services: %w", err)
	}
	item := ProjectDatabaseServices{ProjectID: projectID, Engines: make([]string, 0, 2)}
	if mysqlEnabled != 0 {
		item.Engines = append(item.Engines, "mysql")
	}
	if postgresqlEnabled != 0 {
		item.Engines = append(item.Engines, "postgresql")
	}
	item.UpdatedAt, err = parseDBTime(updated)
	if err != nil {
		return ProjectDatabaseServices{}, err
	}
	return item, nil
}

func (r *Repository) UpsertProjectDatabaseServices(ctx context.Context, item ProjectDatabaseServices) error {
	mysqlEnabled, postgresqlEnabled := 0, 0
	for _, engine := range item.Engines {
		switch engine {
		case "mysql":
			mysqlEnabled = 1
		case "postgresql":
			postgresqlEnabled = 1
		}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO project_database_service_access(project_id,mysql_enabled,postgresql_enabled,updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET
			mysql_enabled=excluded.mysql_enabled,
			postgresql_enabled=excluded.postgresql_enabled,
			updated_at=excluded.updated_at`,
		item.ProjectID, mysqlEnabled, postgresqlEnabled, item.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store project database services: %w", err)
	}
	return nil
}

func (s *Service) GetProjectDatabaseServices(ctx context.Context, projectID string) (ProjectDatabaseServices, error) {
	if _, err := s.repo.ProjectByID(ctx, projectID); err != nil {
		return ProjectDatabaseServices{}, err
	}
	return s.repo.ProjectDatabaseServices(ctx, projectID)
}

func (s *Service) UpdateProjectDatabaseServices(ctx context.Context, projectID string, input ProjectDatabaseServicesInput, actor, remote *string) (ProjectDatabaseServices, error) {
	if _, err := s.repo.ProjectByID(ctx, projectID); err != nil {
		return ProjectDatabaseServices{}, err
	}
	engines, err := normalizeProjectDatabaseServiceEngines(input.Engines)
	if err != nil {
		return ProjectDatabaseServices{}, err
	}
	item := ProjectDatabaseServices{
		ProjectID: projectID,
		Engines:   engines,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.UpsertProjectDatabaseServices(ctx, item); err != nil {
		return ProjectDatabaseServices{}, err
	}
	s.recordAudit(ctx, actor, "project.database_services.update", "project", &projectID, map[string]any{
		"engines": engines,
	}, remote)
	return item, nil
}

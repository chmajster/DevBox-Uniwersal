package plugins

import (
	"context"
	"errors"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

const (
	JobInstallHostMySQL      = "plugin.mysql.install"
	JobInstallHostPostgreSQL = "plugin.postgresql.install"
)

type MySQLInstallJobHandler struct {
	service *Service
}

func NewMySQLInstallJobHandler(service *Service) *MySQLInstallJobHandler {
	return &MySQLInstallJobHandler{service: service}
}

func (h *MySQLInstallJobHandler) Type() string {
	return JobInstallHostMySQL
}

func (h *MySQLInstallJobHandler) Run(ctx context.Context, _ domain.Job) (map[string]any, error) {
	if h.service == nil {
		return nil, errors.New("plugin service is not configured")
	}
	status, err := h.service.InstallMySQL(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"engine":         status.Engine,
		"version":        status.Version,
		"running":        status.Running,
		"host":           status.Host,
		"port":           status.Port,
		"container_host": status.ContainerHost,
		"server_path":    status.ServerPath,
	}, nil
}

type PostgreSQLInstallJobHandler struct {
	service *Service
}

func NewPostgreSQLInstallJobHandler(service *Service) *PostgreSQLInstallJobHandler {
	return &PostgreSQLInstallJobHandler{service: service}
}

func (h *PostgreSQLInstallJobHandler) Type() string {
	return JobInstallHostPostgreSQL
}

func (h *PostgreSQLInstallJobHandler) Run(ctx context.Context, _ domain.Job) (map[string]any, error) {
	if h.service == nil {
		return nil, errors.New("plugin service is not configured")
	}
	status, err := h.service.InstallPostgreSQL(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"engine":         "postgresql",
		"version":        status.Version,
		"running":        status.Running,
		"host":           status.Host,
		"port":           status.Port,
		"container_host": status.ContainerHost,
		"path":           status.Path,
	}, nil
}

func (s *Service) Handlers() []jobs.Handler {
	return []jobs.Handler{
		NewMySQLInstallJobHandler(s),
		NewPostgreSQLInstallJobHandler(s),
	}
}

func (s *Service) QueueMySQLInstall(ctx context.Context, actor *string) (domain.Job, error) {
	if s.jobs == nil {
		return domain.Job{}, errors.New("job engine is not configured")
	}
	status := s.MySQLStatus(ctx)
	if status.Installed {
		return domain.Job{}, errors.New("host MySQL/MariaDB is already installed")
	}
	if conflict := s.mysqlInstallConflict(); conflict != "" {
		return domain.Job{}, errors.New(conflict)
	}
	if !status.Installable {
		return domain.Job{}, errors.New("host MySQL/MariaDB installation is unavailable")
	}
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobInstallHostMySQL,
		RequestedBy: actor,
		Payload:     map[string]any{"purpose": "application_database"},
	})
}

func (s *Service) QueuePostgreSQLInstall(ctx context.Context, actor *string) (domain.Job, error) {
	if s.jobs == nil {
		return domain.Job{}, errors.New("job engine is not configured")
	}
	status := s.PostgreSQLStatus(ctx)
	if status.Installed {
		return domain.Job{}, errors.New("host PostgreSQL is already installed")
	}
	if !status.Installable {
		return domain.Job{}, errors.New("host PostgreSQL installation is unavailable")
	}
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobInstallHostPostgreSQL,
		RequestedBy: actor,
		Payload:     map[string]any{"purpose": "application_database"},
	})
}

package plugins

import (
	"context"
	"errors"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

const (
	JobInstallMySQLContainer      = "plugin.mysql.install"
	JobInstallPostgreSQLContainer = "plugin.postgresql.install"
)

type MySQLInstallJobHandler struct {
	service *Service
}

func NewMySQLInstallJobHandler(service *Service) *MySQLInstallJobHandler {
	return &MySQLInstallJobHandler{service: service}
}

func (h *MySQLInstallJobHandler) Type() string {
	return JobInstallMySQLContainer
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
		"running":        status.Running,
		"port":           status.Port,
		"container_host": status.ContainerHost,
		"container_name": status.ContainerName,
		"image":          status.Image,
		"volume":         status.Volume,
		"network":        status.Network,
	}, nil
}

type PostgreSQLInstallJobHandler struct {
	service *Service
}

func NewPostgreSQLInstallJobHandler(service *Service) *PostgreSQLInstallJobHandler {
	return &PostgreSQLInstallJobHandler{service: service}
}

func (h *PostgreSQLInstallJobHandler) Type() string {
	return JobInstallPostgreSQLContainer
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
		"running":        status.Running,
		"port":           status.Port,
		"container_host": status.ContainerHost,
		"container_name": status.ContainerName,
		"image":          status.Image,
		"volume":         status.Volume,
		"network":        status.Network,
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
		return domain.Job{}, errors.New("MySQL/MariaDB Docker server is already installed")
	}
	if !status.Installable {
		return domain.Job{}, errors.New("MySQL/MariaDB Docker installation is unavailable")
	}
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobInstallMySQLContainer,
		RequestedBy: actor,
		Payload:     map[string]any{"purpose": "application_database", "runtime": "docker"},
	})
}

func (s *Service) QueuePostgreSQLInstall(ctx context.Context, actor *string) (domain.Job, error) {
	if s.jobs == nil {
		return domain.Job{}, errors.New("job engine is not configured")
	}
	status := s.PostgreSQLStatus(ctx)
	if status.Installed {
		return domain.Job{}, errors.New("PostgreSQL Docker server is already installed")
	}
	if !status.Installable {
		return domain.Job{}, errors.New("PostgreSQL Docker installation is unavailable")
	}
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobInstallPostgreSQLContainer,
		RequestedBy: actor,
		Payload:     map[string]any{"purpose": "application_database", "runtime": "docker"},
	})
}

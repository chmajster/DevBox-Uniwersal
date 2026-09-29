package plugins

import (
	"context"
	"errors"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

const JobInstallHostMySQL = "plugin.mysql.install"

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
		"engine":      status.Engine,
		"version":     status.Version,
		"running":     status.Running,
		"host":        status.Host,
		"port":        status.Port,
		"server_path": status.ServerPath,
	}, nil
}

func (s *Service) Handlers() []jobs.Handler {
	return []jobs.Handler{NewMySQLInstallJobHandler(s)}
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
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobInstallHostMySQL,
		RequestedBy: actor,
		Payload:     map[string]any{},
	})
}

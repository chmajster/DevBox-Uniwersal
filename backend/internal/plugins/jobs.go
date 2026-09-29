package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

const (
	JobInstallMySQLContainer       = "plugin.mysql.install"
	JobInstallPostgreSQLContainer  = "plugin.postgresql.install"
	JobMySQLContainerAction        = "plugin.mysql.action"
	JobPostgreSQLContainerAction   = "plugin.postgresql.action"
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
	return mysqlJobResult("install", status), nil
}

type MySQLActionJobHandler struct {
	service *Service
}

func NewMySQLActionJobHandler(service *Service) *MySQLActionJobHandler {
	return &MySQLActionJobHandler{service: service}
}

func (h *MySQLActionJobHandler) Type() string {
	return JobMySQLContainerAction
}

func (h *MySQLActionJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	if h.service == nil {
		return nil, errors.New("plugin service is not configured")
	}
	action, err := jobAction(job)
	if err != nil {
		return nil, err
	}
	status, err := h.service.MySQLAction(ctx, action)
	if err != nil {
		return nil, err
	}
	return mysqlJobResult(action, status), nil
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
	return postgreSQLJobResult("install", status), nil
}

type PostgreSQLActionJobHandler struct {
	service *Service
}

func NewPostgreSQLActionJobHandler(service *Service) *PostgreSQLActionJobHandler {
	return &PostgreSQLActionJobHandler{service: service}
}

func (h *PostgreSQLActionJobHandler) Type() string {
	return JobPostgreSQLContainerAction
}

func (h *PostgreSQLActionJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	if h.service == nil {
		return nil, errors.New("plugin service is not configured")
	}
	action, err := jobAction(job)
	if err != nil {
		return nil, err
	}
	status, err := h.service.PostgreSQLAction(ctx, action)
	if err != nil {
		return nil, err
	}
	return postgreSQLJobResult(action, status), nil
}

func jobAction(job domain.Job) (string, error) {
	value, ok := job.Payload["action"].(string)
	if !ok {
		return "", errors.New("plugin action is missing from job payload")
	}
	return normalizeContainerAction(value)
}

func normalizeContainerAction(action string) (string, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "install", "start", "stop", "restart", "uninstall":
		return action, nil
	default:
		return "", fmt.Errorf("unsupported plugin container action %q", action)
	}
}

func mysqlJobResult(action string, status MySQLPluginStatus) map[string]any {
	return map[string]any{
		"action":         action,
		"engine":         status.Engine,
		"installed":      status.Installed,
		"running":        status.Running,
		"port":           status.Port,
		"container_host": status.ContainerHost,
		"container_name": status.ContainerName,
		"image":          status.Image,
		"volume":         status.Volume,
		"network":        status.Network,
	}
}

func postgreSQLJobResult(action string, status PostgreSQLStatus) map[string]any {
	return map[string]any{
		"action":         action,
		"engine":         "postgresql",
		"installed":      status.Installed,
		"running":        status.Running,
		"port":           status.Port,
		"container_host": status.ContainerHost,
		"container_name": status.ContainerName,
		"image":          status.Image,
		"volume":         status.Volume,
		"network":        status.Network,
	}
}

func (s *Service) Handlers() []jobs.Handler {
	return []jobs.Handler{
		NewMySQLInstallJobHandler(s),
		NewPostgreSQLInstallJobHandler(s),
		NewMySQLActionJobHandler(s),
		NewPostgreSQLActionJobHandler(s),
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

func (s *Service) QueueMySQLAction(ctx context.Context, action string, actor *string) (domain.Job, error) {
	action, err := normalizeContainerAction(action)
	if err != nil {
		return domain.Job{}, err
	}
	if action == "install" {
		return s.QueueMySQLInstall(ctx, actor)
	}
	if s.jobs == nil {
		return domain.Job{}, errors.New("job engine is not configured")
	}
	status := s.MySQLStatus(ctx)
	if !status.Installable {
		return domain.Job{}, errors.New("MySQL/MariaDB Docker lifecycle is unavailable")
	}
	if !status.Installed {
		return domain.Job{}, errors.New("MySQL/MariaDB Docker server is not installed")
	}
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobMySQLContainerAction,
		RequestedBy: actor,
		Payload: map[string]any{
			"action":  action,
			"purpose": "application_database",
			"runtime": "docker",
		},
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

func (s *Service) QueuePostgreSQLAction(ctx context.Context, action string, actor *string) (domain.Job, error) {
	action, err := normalizeContainerAction(action)
	if err != nil {
		return domain.Job{}, err
	}
	if action == "install" {
		return s.QueuePostgreSQLInstall(ctx, actor)
	}
	if s.jobs == nil {
		return domain.Job{}, errors.New("job engine is not configured")
	}
	status := s.PostgreSQLStatus(ctx)
	if !status.Installable {
		return domain.Job{}, errors.New("PostgreSQL Docker lifecycle is unavailable")
	}
	if !status.Installed {
		return domain.Job{}, errors.New("PostgreSQL Docker server is not installed")
	}
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobPostgreSQLContainerAction,
		RequestedBy: actor,
		Payload: map[string]any{
			"action":  action,
			"purpose": "application_database",
			"runtime": "docker",
		},
	})
}

package plugins

import (
	"context"
	"errors"
	"fmt"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

const (
	JobInstallHostMySQL      = "plugin.mysql.install"
	JobInstallHostPostgreSQL = "plugin.postgresql.install"
)

type DatabaseInstallJobHandler struct {
	service *Service
	typ     string
	engine  string
}

func NewDatabaseInstallJobHandler(service *Service, typ, engine string) *DatabaseInstallJobHandler {
	return &DatabaseInstallJobHandler{service: service, typ: typ, engine: engine}
}

func (h *DatabaseInstallJobHandler) Type() string {
	return h.typ
}

func (h *DatabaseInstallJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	if h.service == nil {
		return nil, errors.New("plugin service is not configured")
	}
	port, err := jobPayloadPort(job.Payload)
	if err != nil {
		return nil, err
	}

	switch h.engine {
	case "mysql":
		status, installErr := h.service.InstallMySQL(ctx, port)
		if installErr != nil {
			return nil, installErr
		}
		return map[string]any{
			"engine":            status.Engine,
			"version":           status.Version,
			"running":           status.Running,
			"application_ready": status.ApplicationReady,
			"host":              status.ContainerHost,
			"port":              status.Port,
			"purpose":           status.Purpose,
		}, nil
	case "postgresql":
		status, installErr := h.service.InstallPostgreSQL(ctx, port)
		if installErr != nil {
			return nil, installErr
		}
		return map[string]any{
			"engine":            "postgresql",
			"version":           status.Version,
			"running":           status.Running,
			"application_ready": status.ApplicationReady,
			"host":              status.ContainerHost,
			"port":              status.Port,
			"purpose":           status.Purpose,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported application database engine %q", h.engine)
	}
}

func (s *Service) Handlers() []jobs.Handler {
	return []jobs.Handler{
		NewDatabaseInstallJobHandler(s, JobInstallHostMySQL, "mysql"),
		NewDatabaseInstallJobHandler(s, JobInstallHostPostgreSQL, "postgresql"),
	}
}

func (s *Service) QueueMySQLInstall(ctx context.Context, actor *string, port int) (domain.Job, error) {
	if s.jobs == nil {
		return domain.Job{}, errors.New("job engine is not configured")
	}
	if port == 0 {
		port = s.suggestedApplicationDatabasePort(3306)
	}
	if conflict := s.mysqlInstallConflict(ctx, port); conflict != "" {
		return domain.Job{}, errors.New(conflict)
	}
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobInstallHostMySQL,
		RequestedBy: actor,
		Payload:     map[string]any{"port": port},
	})
}

func (s *Service) QueuePostgreSQLInstall(ctx context.Context, actor *string, port int) (domain.Job, error) {
	if s.jobs == nil {
		return domain.Job{}, errors.New("job engine is not configured")
	}
	if port == 0 {
		port = s.suggestedApplicationDatabasePort(5432)
	}
	if conflict := s.postgresInstallConflict(ctx, port); conflict != "" {
		return domain.Job{}, errors.New(conflict)
	}
	return s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobInstallHostPostgreSQL,
		RequestedBy: actor,
		Payload:     map[string]any{"port": port},
	})
}

func jobPayloadPort(payload map[string]any) (int, error) {
	value, ok := payload["port"]
	if !ok {
		return 0, errors.New("job payload field port is required")
	}
	var port int
	switch raw := value.(type) {
	case int:
		port = raw
	case int64:
		port = int(raw)
	case float64:
		port = int(raw)
	default:
		return 0, errors.New("job payload field port must be numeric")
	}
	if port < 1024 || port > 65535 {
		return 0, fmt.Errorf("invalid application database port %d", port)
	}
	return port, nil
}

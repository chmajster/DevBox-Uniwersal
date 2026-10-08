package databases

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

type DatabaseServerManager interface {
	Action(context.Context, string) error
	ContainerState(context.Context) (bool, bool, error)
	ContainerName() string
	Image() string
	Volume() string
}

func WithDatabaseServers(servers map[string]DatabaseServerManager) ServiceOption {
	return func(s *Service) { s.servers = servers }
}

type DatabaseServer struct {
	Engine    string `json:"engine"`
	Container string `json:"container"`
	Image     string `json:"image"`
	Volume    string `json:"volume"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Error     string `json:"error,omitempty"`
}

func (s *Service) DatabaseServers(ctx context.Context) []DatabaseServer {
	out := []DatabaseServer{}
	for _, engine := range []string{"mysql", "mariadb", "postgresql"} {
		if manager := s.servers[engine]; manager != nil {
			installed, running, err := manager.ContainerState(ctx)
			item := DatabaseServer{Engine: engine, Container: manager.ContainerName(), Image: manager.Image(), Volume: manager.Volume(), Installed: installed, Running: running}
			if err != nil {
				item.Error = err.Error()
			}
			out = append(out, item)
		}
	}
	return out
}

const JobDatabaseServer = "database.server-operation"

func (s *Service) QueueDatabaseServer(ctx context.Context, engine, action string, actor *string) (domain.Job, error) {
	if s.servers[engine] == nil {
		return domain.Job{}, errors.New("database server is not configured")
	}
	if action != "install" && action != "start" && action != "stop" && action != "restart" {
		return domain.Job{}, errors.New("invalid server action")
	}
	return s.jobs.Enqueue(ctx, jobs.Request{Type: JobDatabaseServer, RequestedBy: actor, Payload: map[string]any{"engine": engine, "action": action}})
}

type databaseServerJob struct{ service *Service }

func (h *databaseServerJob) Type() string { return JobDatabaseServer }
func (h *databaseServerJob) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	engine, _ := job.Payload["engine"].(string)
	action, _ := job.Payload["action"].(string)
	manager := h.service.servers[engine]
	if manager == nil {
		return nil, errors.New("server unavailable")
	}
	if err := manager.Action(ctx, action); err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "engine": engine, "action": action}, nil
}
func (s *Service) ensureApplicationDatabaseServer(ctx context.Context, engine string) error {
	if manager := s.servers[normalizeDatabaseEngineName(engine)]; manager != nil {
		if err := manager.Action(ctx, "start"); err != nil {
			return err
		}
	}
	provider, err := s.engineFor(engine)
	if err != nil {
		return err
	}
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err = provider.Health(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("database server did not become ready: %w", err)
		case <-ticker.C:
		}
	}
}

package databases

import (
	"context"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"time"
)

const JobPHPMyAdmin = "database.phpmyadmin.action"

type PHPMyAdminJobHandler struct{ service *Service }

func (h *PHPMyAdminJobHandler) Type() string { return JobPHPMyAdmin }
func (h *PHPMyAdminJobHandler) Run(parent context.Context, j domain.Job) (map[string]any, error) {
	action, _ := j.Payload["action"].(string)
	ctx, cancel := context.WithTimeout(parent, 15*time.Minute)
	defer cancel()
	status, err := h.service.PHPMyAdminAction(ctx, action, j.RequestedBy, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": status}, nil
}
func (s *Service) QueuePHPMyAdminAction(ctx context.Context, action string, actor, remote *string) (domain.Job, error) {
	if s.jobs == nil {
		return domain.Job{}, fmt.Errorf("job engine is not configured")
	}
	if s.phpMyAdmin == nil {
		return domain.Job{}, fmt.Errorf("phpMyAdmin manager is not configured")
	}
	switch action {
	case "install", "start", "stop", "restart":
	default:
		return domain.Job{}, fmt.Errorf("invalid phpMyAdmin action")
	}
	job, err := s.jobs.Enqueue(ctx, jobs.Request{Type: JobPHPMyAdmin, ResourceKey: "global:maintenance", RequestedBy: actor, Payload: map[string]any{"action": action}})
	if err == nil {
		s.recordAudit(ctx, actor, "phpmyadmin."+action+".queued", "job", &job.ID, nil, remote)
	}
	return job, err
}

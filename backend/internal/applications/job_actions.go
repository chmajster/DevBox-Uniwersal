package applications

import (
	"context"
	"fmt"
	"net/http"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	core "github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

func (s *Service) ApplicationJob(ctx context.Context, id, jobID string) (domain.Job, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return domain.Job{}, err
	}
	job, err := core.NewSQLiteJobs(s.repo.db).ByID(ctx, jobID)
	if err != nil || job.ApplicationID == nil || *job.ApplicationID != id {
		return domain.Job{}, ErrNotFound
	}
	return job, nil
}
func (s *Service) RetryApplicationJob(ctx context.Context, id, jobID string, actor *string) (domain.Job, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.jobs == nil {
		return domain.Job{}, ErrProviderUnavailable
	}
	if err := s.repo.CheckIdle(ctx, id); err != nil {
		return domain.Job{}, err
	}
	old, err := s.ApplicationJob(ctx, id, jobID)
	if err != nil {
		return domain.Job{}, err
	}
	if old.Status != "failed" && old.Status != "cancelled" {
		return domain.Job{}, fmt.Errorf("%w: only failed/cancelled jobs can be retried", ErrConflict)
	}
	return s.jobs.Enqueue(ctx, jobs.Request{Type: old.Type, ApplicationID: &id, RequestedBy: actor, Payload: old.Payload})
}
func (m *Module) registerJobRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	mux.Handle("POST /api/v1/applications/{id}/jobs/{jobID}/{action}", middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, http.HandlerFunc(m.jobAction))))
}
func (m *Module) jobAction(w http.ResponseWriter, r *http.Request) {
	id, jobID, action := r.PathValue("id"), r.PathValue("jobID"), r.PathValue("action")
	old, err := m.service.ApplicationJob(r.Context(), id, jobID)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := applicationActor(r)
	if action == "retry" && old.Type == JobRemove {
		user, ok := api.CurrentUser(r.Context())
		if !ok || user.Role != domain.RoleAdmin {
			writeApplicationError(w, http.StatusForbidden, "forbidden", "retrying removal requires administrator", nil)
			return
		}
	}
	var result any
	switch action {
	case "cancel":
		if m.service.jobs == nil {
			m.fail(w, ErrProviderUnavailable)
			return
		}
		err = m.service.jobs.Cancel(r.Context(), jobID)
		result = map[string]any{"status": "cancellation_requested", "job_id": jobID}
	case "retry":
		result, err = m.service.RetryApplicationJob(r.Context(), id, jobID, actor)
	default:
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", "unsupported job action", nil)
		return
	}
	if err != nil {
		m.fail(w, err)
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "application.job."+action, "application", &id, map[string]any{"job_id": jobID}, remoteAddress(r))
	}
	writeApplicationData(w, http.StatusAccepted, result)
}

func (m *Module) applicationJobs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := m.service.repo.Get(r.Context(), id); err != nil {
		m.fail(w, err)
		return
	}
	items, err := core.NewSQLiteJobs(m.service.repo.db).ListApplication(r.Context(), id)
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, items)
}

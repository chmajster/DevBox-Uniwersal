package apphealth

import (
	"net/http"
	"strconv"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Module struct {
	service *Service
	audit   *audit.Service
}

func NewModule(service *Service, auditService *audit.Service) *Module {
	return &Module{service: service, audit: auditService}
}

func (m *Module) Name() string { return "application-health" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, handler))
	}
	operator := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, handler))
	}
	mux.Handle("GET /api/v1/health-checks", viewer(http.HandlerFunc(m.list)))
	mux.Handle("GET /api/v1/health-checks/history", viewer(http.HandlerFunc(m.history)))
	mux.Handle("POST /api/v1/projects/{id}/health-check", operator(http.HandlerFunc(m.runProject)))
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.ListCurrent(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "health_list_failed", err.Error(), nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) history(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := m.service.History(r.Context(), r.URL.Query().Get("project_id"), limit)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "health_history_failed", err.Error(), nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) runProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	item, err := m.service.RunProject(r.Context(), projectID)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "health_check_failed", err.Error(), nil)
		return
	}
	if user, ok := api.CurrentUser(r.Context()); ok && m.audit != nil {
		actor := user.ID
		_ = m.audit.Record(r.Context(), &actor, "project.health_check", "project", &projectID, map[string]any{
			"status": item.Status,
			"target": item.Target,
		}, nil)
	}
	api.WriteJSON(w, http.StatusOK, item)
}

package sourcecontrol

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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

func (m *Module) Name() string { return "source-control-integrations" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	secure := func(role domain.Role, handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(role, handler))
	}
	mux.Handle("GET /api/v1/integrations", secure(domain.RoleViewer, m.list))
	mux.Handle("POST /api/v1/integrations", secure(domain.RoleAdmin, m.create))
	mux.Handle("GET /api/v1/integrations/{id}", secure(domain.RoleViewer, m.get))
	mux.Handle("PUT /api/v1/integrations/{id}", secure(domain.RoleAdmin, m.update))
	mux.Handle("DELETE /api/v1/integrations/{id}", secure(domain.RoleAdmin, m.delete))
	mux.Handle("POST /api/v1/integrations/{id}/test", secure(domain.RoleAdmin, m.test))
	mux.Handle("POST /api/v1/integrations/{id}/sync", secure(domain.RoleAdmin, m.sync))
	mux.Handle("GET /api/v1/integrations/{id}/namespaces", secure(domain.RoleOperator, m.namespaces))
	mux.Handle("GET /api/v1/integrations/{id}/repositories", secure(domain.RoleOperator, m.repositories))
	mux.Handle("GET /api/v1/integrations/{id}/branches", secure(domain.RoleOperator, m.branches))
	mux.Handle("GET /api/v1/projects/{id}/source-control", secure(domain.RoleViewer, m.projectSource))
	mux.Handle("GET /api/v1/projects/{id}/source-control/pull-requests", secure(domain.RoleViewer, m.pullRequests))
	mux.Handle("GET /api/v1/projects/{id}/source-control/ci", secure(domain.RoleViewer, m.ci))
	mux.Handle("GET /api/v1/projects/{id}/source-control/link", secure(domain.RoleViewer, m.webLink))
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.List(r.Context())
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	item, err := m.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	var input IntegrationInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	actor := actorID(r)
	item, err := m.service.Create(r.Context(), input, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if m.audit != nil {
		id := item.ID
		_ = m.audit.Record(r.Context(), actor, "integration.created", "source_control_integration", &id, map[string]any{"provider": item.Provider, "web_url": item.WebURL}, nil)
		_ = m.audit.Record(r.Context(), actor, "integration.connection.tested", "source_control_integration", &id, map[string]any{"status": item.Status, "account": item.AccountUsername}, nil)
	}
	writeJSON(w, http.StatusCreated, item)
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	var input IntegrationInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	item, err := m.service.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	if m.audit != nil {
		id := item.ID
		_ = m.audit.Record(r.Context(), actor, "integration.updated", "source_control_integration", &id, map[string]any{"provider": item.Provider, "status": item.Status}, nil)
		_ = m.audit.Record(r.Context(), actor, "integration.connection.tested", "source_control_integration", &id, map[string]any{"status": item.Status, "account": item.AccountUsername}, nil)
	}
	writeJSON(w, http.StatusOK, item)
}

func (m *Module) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item, err := m.service.Get(r.Context(), id)
	if err != nil {
		m.fail(w, err)
		return
	}
	if err := m.service.Delete(r.Context(), id); err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "integration.deleted", "source_control_integration", &id, map[string]any{"provider": item.Provider, "name": item.Name}, nil)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (m *Module) test(w http.ResponseWriter, r *http.Request) {
	result, err := m.service.Test(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	if m.audit != nil {
		id := r.PathValue("id")
		_ = m.audit.Record(r.Context(), actor, "integration.connection.tested", "source_control_integration", &id, map[string]any{"status": result.Status, "account": result.Account}, nil)
	}
	writeJSON(w, http.StatusOK, result)
}

func (m *Module) sync(w http.ResponseWriter, r *http.Request) {
	actor := actorID(r)
	job, err := m.service.EnqueueSync(r.Context(), r.PathValue("id"), actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (m *Module) namespaces(w http.ResponseWriter, r *http.Request) {
	result, err := m.service.Namespaces(r.Context(), r.PathValue("id"), sanitizeSearch(r.URL.Query().Get("search")), parsePositiveInt(r.URL.Query().Get("page"), 1), parsePositiveInt(r.URL.Query().Get("per_page"), 50))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (m *Module) repositories(w http.ResponseWriter, r *http.Request) {
	result, err := m.service.Repositories(r.Context(), r.PathValue("id"), strings.TrimSpace(r.URL.Query().Get("namespace")), sanitizeSearch(r.URL.Query().Get("search")), parsePositiveInt(r.URL.Query().Get("page"), 1), parsePositiveInt(r.URL.Query().Get("per_page"), 50))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (m *Module) branches(w http.ResponseWriter, r *http.Request) {
	repository := strings.TrimSpace(r.URL.Query().Get("repository"))
	if repository == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "repository query parameter is required", nil)
		return
	}
	result, err := m.service.Branches(r.Context(), r.PathValue("id"), repository, sanitizeSearch(r.URL.Query().Get("search")), parsePositiveInt(r.URL.Query().Get("page"), 1), parsePositiveInt(r.URL.Query().Get("per_page"), 50))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (m *Module) projectSource(w http.ResponseWriter, r *http.Request) {
	item, err := m.service.ProjectSource(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (m *Module) pullRequests(w http.ResponseWriter, r *http.Request) {
	result, err := m.service.PullRequests(r.Context(), r.PathValue("id"), strings.TrimSpace(r.URL.Query().Get("state")), parsePositiveInt(r.URL.Query().Get("page"), 1), parsePositiveInt(r.URL.Query().Get("per_page"), 30))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (m *Module) ci(w http.ResponseWriter, r *http.Request) {
	item, err := m.service.CI(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (m *Module) webLink(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	identifier := strings.TrimSpace(r.URL.Query().Get("identifier"))
	value, err := m.service.ProjectWebLink(r.Context(), r.PathValue("id"), kind, identifier)
	if err != nil {
		m.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": value})
}

func (m *Module) fail(w http.ResponseWriter, err error) {
	var rate *RateLimitError
	switch {
	case errors.As(err, &rate):
		details := map[string]any{"remaining": rate.Remaining}
		if !rate.ResetAt.IsZero() {
			details["reset_at"] = rate.ResetAt.UTC().Format(time.RFC3339)
		}
		writeError(w, http.StatusTooManyRequests, "rate_limited", err.Error(), details)
	case errors.Is(err, ErrIntegrationNotFound):
		writeError(w, http.StatusNotFound, "integration_not_found", "source-control integration was not found", nil)
	case errors.Is(err, ErrIntegrationInUse):
		writeError(w, http.StatusConflict, "integration_in_use", err.Error(), nil)
	case errors.Is(err, ErrInvalidIntegration):
		writeError(w, http.StatusBadRequest, "invalid_integration", err.Error(), nil)
	case errors.Is(err, ErrAuthentication):
		writeError(w, http.StatusUnauthorized, "provider_authentication_failed", err.Error(), nil)
	case errors.Is(err, ErrPermission):
		writeError(w, http.StatusForbidden, "provider_permission_denied", err.Error(), nil)
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "provider_resource_not_found", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "source_control_error", err.Error(), nil)
	}
}

func actorID(r *http.Request) *string {
	user, ok := api.CurrentUser(r.Context())
	if !ok {
		return nil
	}
	id := user.ID
	return &id
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON request")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	errorBody := map[string]any{"code": code, "message": message}
	if details != nil {
		errorBody["details"] = details
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"error": errorBody})
}

package runtimes

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type VersionModule struct {
	service *VersionService
	audit   *audit.Service
}

func NewVersionModule(service *VersionService, auditService *audit.Service) *VersionModule {
	return &VersionModule{service: service, audit: auditService}
}

func (m *VersionModule) Name() string { return "runtime-versions" }

func (m *VersionModule) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	secure := func(role domain.Role, handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(role, handler))
	}
	mux.Handle("GET /api/v1/runtimes/{type}", secure(domain.RoleViewer, m.runtime))
	mux.Handle("GET /api/v1/runtimes/{type}/available", secure(domain.RoleViewer, m.available))
	mux.Handle("GET /api/v1/runtimes/{type}/installed", secure(domain.RoleViewer, m.installed))
	mux.Handle("POST /api/v1/runtimes/{type}/install", secure(domain.RoleAdmin, m.install))
	mux.Handle("DELETE /api/v1/runtimes/{type}/installations/{id}", secure(domain.RoleAdmin, m.remove))
	mux.Handle("POST /api/v1/runtimes/installations/{id}/validate", secure(domain.RoleAdmin, m.validate))
	mux.Handle("GET /api/v1/runtime-defaults", secure(domain.RoleViewer, m.defaults))
	mux.Handle("PUT /api/v1/runtime-defaults/{type}", secure(domain.RoleAdmin, m.setDefault))
	mux.Handle("GET /api/v1/projects/{id}/runtimes", secure(domain.RoleViewer, m.projectRuntimes))
	mux.Handle("PUT /api/v1/projects/{id}/runtimes/{type}", secure(domain.RoleOperator, m.setProjectRuntime))
	mux.Handle("POST /api/v1/projects/{id}/runtimes/python/venv/recreate", secure(domain.RoleOperator, m.recreateVenv))
}

func (m *VersionModule) runtime(w http.ResponseWriter, r *http.Request) {
	view, err := m.service.Runtime(r.Context(), r.PathValue("type"), r.URL.Query().Get("available") == "true")
	if err != nil {
		m.fail(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, view)
}

func (m *VersionModule) available(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Available(r.Context(), r.PathValue("type"), r.URL.Query().Get("refresh") == "true")
	if err != nil {
		m.fail(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, items)
}

func (m *VersionModule) installed(w http.ResponseWriter, r *http.Request) {
	view, err := m.service.Runtime(r.Context(), r.PathValue("type"), false)
	if err != nil {
		m.fail(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, view.Installations)
}

func (m *VersionModule) install(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version string `json:"version"`
	}
	if err := decodeRuntimeJSON(w, r, &input); err != nil {
		writeRuntimeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	actor := runtimeActorID(r)
	installation, job, err := m.service.EnqueueInstall(r.Context(), r.PathValue("type"), input.Version, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	id := installation.ID
	if m.audit != nil {
		if err := m.audit.Record(r.Context(), actor, "runtime.install.requested", "runtime_installation", &id, map[string]any{"runtime_type": installation.RuntimeType, "version": installation.Version, "job_id": job.ID}, nil); err != nil {
			writeRuntimeError(w, http.StatusInternalServerError, "audit_failed", "runtime install queued but audit persistence failed")
			return
		}
	}
	writeRuntimeJSON(w, http.StatusAccepted, map[string]any{"installation": installation, "job": job})
}

func (m *VersionModule) remove(w http.ResponseWriter, r *http.Request) {
	installation, err := m.service.repo.GetInstallation(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	if installation.RuntimeType != normalizeRuntimeType(r.PathValue("type")) {
		writeRuntimeError(w, http.StatusNotFound, "runtime_installation_not_found", "runtime installation was not found")
		return
	}
	actor := runtimeActorID(r)
	job, err := m.service.EnqueueRemove(r.Context(), installation.ID, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusAccepted, job)
}

func (m *VersionModule) validate(w http.ResponseWriter, r *http.Request) {
	installation, err := m.service.Validate(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := runtimeActorID(r)
	id := installation.ID
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "runtime.validate", "runtime_installation", &id, map[string]any{"status": installation.Status}, nil)
	}
	writeRuntimeJSON(w, http.StatusOK, installation)
}

func (m *VersionModule) defaults(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Defaults(r.Context())
	if err != nil {
		m.fail(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, items)
}

func (m *VersionModule) setDefault(w http.ResponseWriter, r *http.Request) {
	var input struct {
		InstallationID string `json:"runtime_installation_id"`
	}
	if err := decodeRuntimeJSON(w, r, &input); err != nil {
		writeRuntimeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	actor := runtimeActorID(r)
	item, err := m.service.SetDefault(r.Context(), r.PathValue("type"), strings.TrimSpace(input.InstallationID), actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if m.audit != nil {
		id := item.InstallationID
		_ = m.audit.Record(r.Context(), actor, "runtime.default.changed", "runtime_installation", &id, map[string]any{"runtime_type": item.RuntimeType, "version": item.ResolvedVersion}, nil)
	}
	writeRuntimeJSON(w, http.StatusOK, item)
}

func (m *VersionModule) projectRuntimes(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.ProjectRuntimes(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, items)
}

func (m *VersionModule) setProjectRuntime(w http.ResponseWriter, r *http.Request) {
	var input struct {
		InstallationID string `json:"runtime_installation_id"`
	}
	if err := decodeRuntimeJSON(w, r, &input); err != nil {
		writeRuntimeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	projectID := r.PathValue("id")
	runtimeType := r.PathValue("type")
	item, err := m.service.SetProjectRuntime(r.Context(), projectID, runtimeType, strings.TrimSpace(input.InstallationID))
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := runtimeActorID(r)
	if m.audit != nil {
		metadata := map[string]any{"runtime_type": normalizeRuntimeType(runtimeType), "runtime_installation_id": strings.TrimSpace(input.InstallationID)}
		_ = m.audit.Record(r.Context(), actor, "project.runtime.changed", "project", &projectID, metadata, nil)
	}
	writeRuntimeJSON(w, http.StatusOK, item)
}

func (m *VersionModule) recreateVenv(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeRuntimeJSON(w, r, &input); err != nil || !input.Confirm {
		writeRuntimeError(w, http.StatusBadRequest, "confirmation_required", "confirm=true is required to recreate the project virtualenv")
		return
	}
	projectID := r.PathValue("id")
	if err := m.service.RecreatePythonVenv(r.Context(), projectID); err != nil {
		m.fail(w, err)
		return
	}
	actor := runtimeActorID(r)
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "project.python_venv.recreated", "project", &projectID, nil, nil)
	}
	writeRuntimeJSON(w, http.StatusOK, map[string]any{"status": "removed", "message": "virtualenv removed; dependency installation will recreate it with the assigned Python runtime"})
}

func (m *VersionModule) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInstallationNotFound):
		writeRuntimeError(w, http.StatusNotFound, "runtime_installation_not_found", "runtime installation was not found")
	case errors.Is(err, ErrProjectNotFound):
		writeRuntimeError(w, http.StatusNotFound, "project_not_found", "project was not found")
	case errors.Is(err, ErrRuntimeInUse):
		writeRuntimeError(w, http.StatusConflict, "runtime_in_use", err.Error())
	case errors.Is(err, ErrRuntimeConflict):
		writeRuntimeError(w, http.StatusConflict, "runtime_conflict", err.Error())
	case errors.Is(err, ErrInvalidRuntime):
		writeRuntimeError(w, http.StatusBadRequest, "invalid_runtime", err.Error())
	case errors.Is(err, ErrNoRuntimeAssignment):
		writeRuntimeError(w, http.StatusNotFound, "runtime_assignment_not_found", "runtime is not assigned to the project")
	default:
		writeRuntimeError(w, http.StatusInternalServerError, "runtime_version_error", err.Error())
	}
}

func decodeRuntimeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON request")
	}
	return nil
}

func runtimeActorID(r *http.Request) *string {
	user, ok := api.CurrentUser(r.Context())
	if !ok {
		return nil
	}
	id := user.ID
	return &id
}

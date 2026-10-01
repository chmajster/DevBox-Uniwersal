package projects

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

func (m *Module) Name() string { return "projects" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	m.registerPortRoutes(mux, middleware)
	secure := func(role domain.Role, handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(role, handler))
	}
	mux.Handle("GET /api/v1/projects", secure(domain.RoleViewer, m.list))
	mux.Handle("POST /api/v1/projects", secure(domain.RoleOperator, m.create))
	mux.Handle("POST /api/v1/projects/import", secure(domain.RoleOperator, m.importLocal))
	mux.Handle("GET /api/v1/project-directories", secure(domain.RoleOperator, m.browseDirectories))
	mux.Handle("POST /api/v1/project-directories", secure(domain.RoleOperator, m.createDirectory))
	mux.Handle("GET /api/v1/projects/directories", secure(domain.RoleOperator, m.browseDirectories)) // legacy alias
	mux.Handle("GET /api/v1/projects/{id}", secure(domain.RoleViewer, m.get))
	mux.Handle("GET /api/v1/projects/{id}/files", secure(domain.RoleViewer, m.files))
	mux.Handle("PATCH /api/v1/projects/{id}", secure(domain.RoleOperator, m.update))
	mux.Handle("DELETE /api/v1/projects/{id}", secure(domain.RoleAdmin, m.delete))
	mux.Handle("POST /api/v1/projects/{id}/archive", secure(domain.RoleOperator, m.archive))
	mux.Handle("GET /api/v1/projects/{id}/git", secure(domain.RoleViewer, m.gitState))
	mux.Handle("POST /api/v1/projects/{id}/git/fetch", secure(domain.RoleOperator, m.gitFetch))
	mux.Handle("POST /api/v1/projects/{id}/git/pull", secure(domain.RoleOperator, m.gitPull))
	mux.Handle("POST /api/v1/projects/{id}/git/checkout", secure(domain.RoleOperator, m.gitCheckout))
	mux.Handle("POST /api/v1/projects/{id}/deploy", secure(domain.RoleOperator, m.deploy))
	mux.Handle("GET /api/v1/projects/{id}/deployments", secure(domain.RoleViewer, m.deployments))
	mux.Handle("GET /api/v1/projects/{id}/runtime/config", secure(domain.RoleViewer, m.runtimeConfig))
	mux.Handle("PUT /api/v1/projects/{id}/runtime/config", secure(domain.RoleOperator, m.updateRuntimeConfig))
	mux.Handle("POST /api/v1/projects/{id}/runtime/rebuild", secure(domain.RoleOperator, m.rebuildRuntime))
	mux.Handle("POST /api/v1/projects/{id}/runtime/generate-compose", secure(domain.RoleOperator, m.generateRuntimeCompose))
	mux.Handle("GET /api/v1/runtimes/{runtime}/modules", secure(domain.RoleViewer, m.runtimeModules))
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.List(r.Context(), r.URL.Query().Get("archived") == "true")
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (m *Module) browseDirectories(w http.ResponseWriter, r *http.Request) {
	if values, ok := r.URL.Query()["suggest"]; ok {
		requestedPath := ""
		if len(values) > 0 {
			requestedPath = values[0]
		}
		suggestions, err := m.service.SuggestDirectories(requestedPath)
		if err != nil {
			m.fail(w, err)
			return
		}
		writeData(w, http.StatusOK, suggestions)
		return
	}

	requestedPath := r.URL.Query().Get("path")
	listing, err := m.service.BrowseDirectories(requestedPath)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	targetID := listing.Path
	if targetID == "" {
		targetID = "browse-roots"
	}
	if err := m.audit.Record(r.Context(), actor, "project.directory_browse", "directory", &targetID, map[string]any{"requested_path": requestedPath}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "directory listing succeeded but audit persistence failed")
		return
	}
	writeData(w, http.StatusOK, listing)
}

func (m *Module) createDirectory(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	entry, err := m.service.CreateDirectory(input.Parent, input.Name)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	targetID := entry.Path
	if err := m.audit.Record(r.Context(), actor, "project.directory_create", "directory", &targetID, map[string]any{
		"parent": input.Parent,
		"name":   input.Name,
	}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "directory created but audit persistence failed")
		return
	}
	writeData(w, http.StatusCreated, entry)
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	actor := actorID(r)
	project, job, err := m.service.Create(r.Context(), input, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if err := m.audit.Record(r.Context(), actor, "project.create", "project", &project.ID, map[string]any{"source_type": project.SourceType}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "project created but audit persistence failed")
		return
	}
	status := http.StatusCreated
	if job != nil {
		status = http.StatusAccepted
	}
	writeData(w, status, project)
}

func (m *Module) importLocal(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	input.SourceType = SourceLocal
	actor := actorID(r)
	project, _, err := m.service.Create(r.Context(), input, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if err := m.audit.Record(r.Context(), actor, "project.import", "project", &project.ID, map[string]any{"local_path": project.LocalPath}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "project imported but audit persistence failed")
		return
	}
	writeData(w, http.StatusCreated, project)
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	// Defensive compatibility for deployments/proxies that route the old
	// /projects/directories path through the dynamic {id} handler.
	if r.PathValue("id") == "directories" {
		m.browseDirectories(w, r)
		return
	}
	project, err := m.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, project)
}
func (m *Module) files(w http.ResponseWriter, r *http.Request) {
	listing, err := m.service.BrowseProjectFiles(r.Context(), r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, listing)
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	var input UpdateInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	project, err := m.service.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	if err := m.audit.Record(r.Context(), actor, "project.update", "project", &project.ID, nil, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "project updated but audit persistence failed")
		return
	}
	writeData(w, http.StatusOK, project)
}
func (m *Module) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := m.service.Delete(r.Context(), id); err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	if err := m.audit.Record(r.Context(), actor, "project.delete", "project", &id, nil, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "project deleted but audit persistence failed")
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (m *Module) archive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := m.service.Archive(r.Context(), id); err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	if err := m.audit.Record(r.Context(), actor, "project.archive", "project", &id, nil, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "project archived but audit persistence failed")
		return
	}
	project, err := m.service.Get(r.Context(), id)
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, project)
}
func (m *Module) gitState(w http.ResponseWriter, r *http.Request) {
	state, err := m.service.GitState(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, state)
}
func (m *Module) gitFetch(w http.ResponseWriter, r *http.Request) { m.enqueueGit(w, r, "fetch") }
func (m *Module) gitPull(w http.ResponseWriter, r *http.Request)  { m.enqueueGit(w, r, "pull") }
func (m *Module) enqueueGit(w http.ResponseWriter, r *http.Request, operation string) {
	id := r.PathValue("id")
	actor := actorID(r)
	job, err := m.service.EnqueueGit(r.Context(), id, operation, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if err := m.audit.Record(r.Context(), actor, "project.git."+operation, "project", &id, map[string]any{"job_id": job.ID}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "Git job queued but audit persistence failed")
		return
	}
	writeData(w, http.StatusAccepted, job)
}
func (m *Module) gitCheckout(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Branch string `json:"branch"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	id := r.PathValue("id")
	actor := actorID(r)
	job, err := m.service.EnqueueCheckout(r.Context(), id, input.Branch, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if err := m.audit.Record(r.Context(), actor, "project.git.checkout", "project", &id, map[string]any{"job_id": job.ID, "branch": input.Branch}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "checkout queued but audit persistence failed")
		return
	}
	writeData(w, http.StatusAccepted, job)
}
func (m *Module) deploy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor := actorID(r)
	job, err := m.service.Deploy(r.Context(), id, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if err := m.audit.Record(r.Context(), actor, "project.deploy", "project", &id, map[string]any{"job_id": job.ID}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "deployment queued but audit persistence failed")
		return
	}
	writeData(w, http.StatusAccepted, job)
}
func (m *Module) runtimeConfig(w http.ResponseWriter, r *http.Request) {
	config, err := m.service.RuntimeContainerConfig(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, config)
}

func (m *Module) updateRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	var input RuntimeContainerConfig
	if err := decodeJSON(w, r, &input); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	id := r.PathValue("id")
	config, err := m.service.UpdateRuntimeContainerConfig(r.Context(), id, input)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	if err := m.audit.Record(r.Context(), actor, "project.runtime_config.update", "project", &id, map[string]any{
		"runtime": config.Runtime, "runtime_version": config.RuntimeVersion, "container_policy": config.ContainerPolicy, "modules": len(config.Modules),
	}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "runtime configuration updated but audit persistence failed")
		return
	}
	writeData(w, http.StatusOK, config)
}

func (m *Module) rebuildRuntime(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor := actorID(r)
	job, err := m.service.RebuildRuntime(r.Context(), id, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if err := m.audit.Record(r.Context(), actor, "project.runtime.rebuild", "project", &id, map[string]any{"job_id": job.ID}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "runtime rebuild queued but audit persistence failed")
		return
	}
	writeData(w, http.StatusAccepted, job)
}

func (m *Module) generateRuntimeCompose(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result, err := m.service.GenerateRuntimeCompose(r.Context(), id)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := actorID(r)
	if err := m.audit.Record(r.Context(), actor, "project.runtime.compose.generate", "project", &id, map[string]any{
		"compose_path": result.ComposePath, "dockerfile_path": result.DockerfilePath, "runtime": result.Runtime,
	}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "Compose files generated but audit persistence failed")
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (m *Module) runtimeModules(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.RuntimeModules(r.PathValue("runtime"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (m *Module) deployments(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Deployments(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}
func (m *Module) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "project not found")
	case errors.Is(err, ErrDirectoryAccess):
		writeAPIError(w, http.StatusForbidden, "directory_access_denied", err.Error())
	case errors.Is(err, ErrInvalidInput):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, ErrProviderUnavailable), strings.Contains(err.Error(), "provider unavailable"):
		writeAPIError(w, http.StatusServiceUnavailable, "provider_unavailable", err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "unique constraint"):
		writeAPIError(w, http.StatusConflict, "conflict", "project name or slug already exists")
	default:
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error())
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
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid JSON request")
	}
	return nil
}

type responseEnvelope struct {
	Data  any            `json:"data,omitempty"`
	Error *responseError `json:"error,omitempty"`
}
type responseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(responseEnvelope{Data: data})
}
func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(responseEnvelope{Error: &responseError{Code: code, Message: message}})
}

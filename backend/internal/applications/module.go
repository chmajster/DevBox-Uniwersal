package applications

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
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
func (m *Module) Name() string { return "applications" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	m.registerSecretRoutes(mux, middleware)
	m.registerJobRoutes(mux, middleware)
	secure := func(role domain.Role, handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(role, handler))
	}
	mux.Handle("GET /api/v1/applications", secure(domain.RoleViewer, m.list))
	mux.Handle("POST /api/v1/applications", secure(domain.RoleOperator, m.create))
	mux.Handle("POST /api/v1/applications/detect", secure(domain.RoleOperator, m.detect))
	mux.Handle("GET /api/v1/filesystem/directories", secure(domain.RoleOperator, m.browseDirectories))
	mux.Handle("POST /api/v1/filesystem/directories", secure(domain.RoleOperator, m.createDirectory))
	mux.Handle("GET /api/v1/applications/{id}", secure(domain.RoleViewer, m.get))
	mux.Handle("PATCH /api/v1/applications/{id}", secure(domain.RoleOperator, m.update))
	mux.Handle("DELETE /api/v1/applications/{id}", secure(domain.RoleAdmin, m.remove))
	mux.Handle("POST /api/v1/applications/{id}/deploy", secure(domain.RoleOperator, m.deploy))
	mux.Handle("POST /api/v1/applications/{id}/start", secure(domain.RoleOperator, m.start))
	mux.Handle("POST /api/v1/applications/{id}/stop", secure(domain.RoleOperator, m.stop))
	mux.Handle("POST /api/v1/applications/{id}/restart", secure(domain.RoleOperator, m.restart))
	mux.Handle("POST /api/v1/applications/{id}/reconcile", secure(domain.RoleOperator, m.reconcile))
	mux.Handle("GET /api/v1/applications/{id}/state", secure(domain.RoleViewer, m.state))
	mux.Handle("GET /api/v1/applications/{id}/workloads", secure(domain.RoleViewer, m.workloads))
	mux.Handle("GET /api/v1/applications/{id}/endpoints", secure(domain.RoleViewer, m.endpoints))
	mux.Handle("GET /api/v1/applications/{id}/deployments", secure(domain.RoleViewer, m.deployments))
	mux.Handle("GET /api/v1/applications/{id}/events", secure(domain.RoleViewer, m.events))
	mux.Handle("GET /api/v1/applications/{id}/logs", secure(domain.RoleViewer, m.logs))
}

func (m *Module) browseDirectories(w http.ResponseWriter, r *http.Request) {
	listing, err := m.service.BrowseDirectories(strings.TrimSpace(r.URL.Query().Get("path")))
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := applicationActor(r)
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "application.directory_browse", "directory", nil, map[string]any{"path": listing.Path}, remoteAddress(r))
	}
	writeApplicationData(w, http.StatusOK, listing)
}

func (m *Module) createDirectory(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if err := decodeApplicationJSON(w, r, &input, false); err != nil {
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	entry, err := m.service.CreateDirectory(input.Parent, input.Name)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := applicationActor(r)
	if m.audit != nil {
		target := entry.Path
		_ = m.audit.Record(r.Context(), actor, "application.directory_create", "directory", &target, map[string]any{"parent": input.Parent, "name": input.Name}, remoteAddress(r))
	}
	writeApplicationData(w, http.StatusCreated, entry)
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.List(r.Context())
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, items)
}
func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	item, err := m.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, item)
}
func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := decodeApplicationJSON(w, r, &input, false); err != nil {
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	actor := applicationActor(r)
	item, err := m.service.Create(r.Context(), input, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "application.create", "application", &item.ID, map[string]any{"source_type": item.SourceType}, remoteAddress(r))
	}
	writeApplicationData(w, http.StatusCreated, item)
}
func (m *Module) detect(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := decodeApplicationJSON(w, r, &input, false); err != nil {
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	actor := applicationActor(r)
	result, err := m.service.Detect(r.Context(), input, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	status := http.StatusOK
	if result.Job != nil {
		status = http.StatusAccepted
	}
	writeApplicationData(w, status, result)
}
func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	var input UpdateInput
	if err := decodeApplicationJSON(w, r, &input, false); err != nil {
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	id := r.PathValue("id")
	item, err := m.service.Update(r.Context(), id, input)
	if err != nil {
		m.fail(w, err)
		return
	}
	actor := applicationActor(r)
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "application.update", "application", &id, nil, remoteAddress(r))
	}
	writeApplicationData(w, http.StatusOK, item)
}
func (m *Module) deploy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor := applicationActor(r)
	deployment, job, err := m.service.EnqueueDeploy(r.Context(), id, actor)
	if err != nil {
		m.fail(w, err)
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "application.deploy", "application", &id, map[string]any{"deployment_id": deployment.ID, "job_id": job.ID}, remoteAddress(r))
	}
	writeApplicationData(w, http.StatusAccepted, map[string]any{"deployment": deployment, "job": job})
}
func (m *Module) start(w http.ResponseWriter, r *http.Request)   { m.lifecycle(w, r, "start", nil) }
func (m *Module) stop(w http.ResponseWriter, r *http.Request)    { m.lifecycle(w, r, "stop", nil) }
func (m *Module) restart(w http.ResponseWriter, r *http.Request) { m.lifecycle(w, r, "restart", nil) }
func (m *Module) reconcile(w http.ResponseWriter, r *http.Request) {
	m.lifecycle(w, r, "reconcile", nil)
}
func (m *Module) remove(w http.ResponseWriter, r *http.Request) {
	options := DeleteOptions{RemoveContainers: true, DeleteConfiguration: true}
	if r.ContentLength != 0 {
		if err := decodeApplicationJSON(w, r, &options, true); err != nil {
			writeApplicationError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
	}
	if options.RemoveGeneratedImages || options.RemoveVolumes {
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", "shared images and persistent volumes are preserved; remove them explicitly in Docker administration", nil)
		return
	}
	if !options.RemoveContainers && !options.DeleteConfiguration && !options.RemoveGeneratedImages && !options.RemoveVolumes && !options.RemoveSource {
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", "at least one delete action must be selected", nil)
		return
	}
	m.lifecycle(w, r, "remove", map[string]any{"options": options})
}
func (m *Module) lifecycle(w http.ResponseWriter, r *http.Request, action string, payload map[string]any) {
	id := r.PathValue("id")
	actor := applicationActor(r)
	job, err := m.service.EnqueueLifecycle(r.Context(), id, action, actor, payload)
	if err != nil {
		m.fail(w, err)
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "application."+action, "application", &id, map[string]any{"job_id": job.ID}, remoteAddress(r))
	}
	writeApplicationData(w, http.StatusAccepted, job)
}
func (m *Module) state(w http.ResponseWriter, r *http.Request) {
	item, err := m.service.State(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, item)
}
func (m *Module) workloads(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Workloads(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, items)
}
func (m *Module) endpoints(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Endpoints(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, items)
}
func (m *Module) deployments(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Deployments(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, items)
}
func (m *Module) events(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Events(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, items)
}
func (m *Module) logs(w http.ResponseWriter, r *http.Request) {
	tail := 200
	if raw := r.URL.Query().Get("tail"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			tail = value
		}
	}
	items, err := m.service.Logs(r.Context(), r.PathValue("id"), tail, strings.TrimSpace(r.URL.Query().Get("workload")))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeApplicationData(w, http.StatusOK, items)
}

func (m *Module) fail(w http.ResponseWriter, err error) {
	var operation *OperationError
	switch {
	case errors.Is(err, ErrNotFound):
		writeApplicationError(w, http.StatusNotFound, "not_found", "application not found", nil)
	case errors.Is(err, ErrDirectoryAccess):
		writeApplicationError(w, http.StatusForbidden, "directory_access_denied", err.Error(), nil)
	case errors.Is(err, ErrInvalidInput):
		writeApplicationError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
	case errors.Is(err, ErrConflict):
		writeApplicationError(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, ErrConfigurationRequired):
		writeApplicationError(w, http.StatusConflict, "configuration_required", err.Error(), nil)
	case errors.Is(err, ErrProviderUnavailable), strings.Contains(strings.ToLower(err.Error()), "provider unavailable"):
		writeApplicationError(w, http.StatusServiceUnavailable, "provider_unavailable", err.Error(), nil)
	case errors.As(err, &operation):
		writeApplicationError(w, http.StatusUnprocessableEntity, "operation_failed", operation.Error(), operation)
	default:
		writeApplicationError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
	}
}

func applicationActor(r *http.Request) *string {
	user, ok := api.CurrentUser(r.Context())
	if !ok {
		return nil
	}
	id := user.ID
	return &id
}
func remoteAddress(r *http.Request) *string {
	value := strings.TrimSpace(r.RemoteAddr)
	if value == "" {
		return nil
	}
	return &value
}
func decodeApplicationJSON(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) error {
	if allowEmpty && r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		return errors.New("invalid JSON request")
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("request must contain exactly one JSON document")
	}
	return nil
}
func writeApplicationData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func writeApplicationError(w http.ResponseWriter, status int, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	payload := map[string]any{"error": map[string]any{"code": code, "message": message}}
	if details != nil {
		payload["error"].(map[string]any)["details"] = details
	}
	_ = json.NewEncoder(w).Encode(payload)
}

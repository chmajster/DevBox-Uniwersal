package docker

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Module struct {
	service *Service
}

func NewModule(service *Service) *Module { return &Module{service: service} }

func (m *Module) Name() string { return "docker" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, handler))
	}
	operator := func(handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, handler))
	}
	admin := func(handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleAdmin, handler))
	}

	mux.Handle("GET /api/v1/docker/status", viewer(m.status))
	mux.Handle("GET /api/v1/docker/containers", viewer(m.containers))
	mux.Handle("GET /api/v1/docker/containers/{id}", viewer(m.container))
	mux.Handle("POST /api/v1/docker/containers/{id}/start", operator(m.startContainer))
	mux.Handle("POST /api/v1/docker/containers/{id}/stop", operator(m.stopContainer))
	mux.Handle("POST /api/v1/docker/containers/{id}/restart", operator(m.restartContainer))
	mux.Handle("DELETE /api/v1/docker/containers/{id}", admin(m.removeContainer))
	mux.Handle("GET /api/v1/docker/containers/{id}/logs", viewer(m.containerLogs))
	mux.Handle("POST /api/v1/docker/containers/{id}/exec", operator(m.containerExec))
	mux.Handle("GET /api/v1/docker/images", viewer(m.images))
	mux.Handle("GET /api/v1/docker/volumes", viewer(m.volumes))
	mux.Handle("GET /api/v1/docker/networks", viewer(m.networks))
	mux.Handle("GET /api/v1/docker/compose/projects", viewer(m.composeProjects))
	mux.Handle("GET /api/v1/docker/compose/projects/{name}/ps", viewer(m.composePS))
	mux.Handle("GET /api/v1/docker/compose/projects/{name}/logs", viewer(m.composeLogs))
}

func (m *Module) status(w http.ResponseWriter, r *http.Request) {
	api.WriteJSON(w, http.StatusOK, m.service.Status(r.Context()))
}

func (m *Module) containers(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Containers(r.Context())
	if err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) container(w http.ResponseWriter, r *http.Request) {
	item, err := m.service.Container(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, item)
}

func (m *Module) startContainer(w http.ResponseWriter, r *http.Request) {
	m.containerAction(w, r, "start")
}

func (m *Module) stopContainer(w http.ResponseWriter, r *http.Request) {
	m.containerAction(w, r, "stop")
}

func (m *Module) restartContainer(w http.ResponseWriter, r *http.Request) {
	m.containerAction(w, r, "restart")
}

func (m *Module) removeContainer(w http.ResponseWriter, r *http.Request) {
	m.containerAction(w, r, "remove")
}

func (m *Module) containerAction(w http.ResponseWriter, r *http.Request, action string) {
	user, ok := api.CurrentUser(r.Context())
	if !ok {
		api.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
		return
	}
	id := r.PathValue("id")
	if err := m.service.ContainerAction(r.Context(), user.ID, r.RemoteAddr, id, action); err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"id": id, "action": action, "status": "completed"})
}

func (m *Module) containerLogs(w http.ResponseWriter, r *http.Request) {
	tail := parseTail(r.URL.Query().Get("tail"))
	stream, err := m.service.ContainerLogs(r.Context(), r.PathValue("id"), tail)
	if err != nil {
		writeDockerError(w, err)
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, 4<<20))
	if err != nil {
		api.WriteError(w, http.StatusBadGateway, "docker_logs_failed", "failed to read container logs", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"logs": string(data)})
}

func (m *Module) containerExec(w http.ResponseWriter, r *http.Request) {
	user, ok := api.CurrentUser(r.Context())
	if !ok {
		api.WriteError(w, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
		return
	}
	var body struct {
		Command ExecCommand `json:"command"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid_request", "invalid exec request", nil)
		return
	}
	output, err := m.service.ContainerExec(r.Context(), user.ID, r.RemoteAddr, r.PathValue("id"), body.Command)
	if err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"output": output})
}

func (m *Module) images(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Images(r.Context())
	if err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) volumes(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Volumes(r.Context())
	if err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) networks(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.Networks(r.Context())
	if err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) composeProjects(w http.ResponseWriter, _ *http.Request) {
	items, err := m.service.ComposeProjects()
	if err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) composePS(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.ComposePS(r.Context(), r.PathValue("name"))
	if err != nil {
		writeDockerError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) composeLogs(w http.ResponseWriter, r *http.Request) {
	tail := parseTail(r.URL.Query().Get("tail"))
	service := r.URL.Query().Get("service")
	stream, err := m.service.ComposeLogs(r.Context(), r.PathValue("name"), service, tail)
	if err != nil {
		writeDockerError(w, err)
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, 4<<20))
	if err != nil {
		api.WriteError(w, http.StatusBadGateway, "docker_logs_failed", "failed to read compose logs", nil)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]string{"logs": string(data)})
}

func parseTail(value string) int {
	if value == "" {
		return 200
	}
	tail, err := strconv.Atoi(value)
	if err != nil || tail < 1 {
		return 200
	}
	if tail > 5000 {
		return 5000
	}
	return tail
}

func writeDockerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		api.WriteError(w, http.StatusBadRequest, "invalid_docker_input", err.Error(), nil)
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "docker_resource_not_found", "docker resource not found", nil)
	case errors.Is(err, ErrUnavailable):
		api.WriteError(w, http.StatusServiceUnavailable, "docker_unavailable", "docker engine is unavailable", nil)
	default:
		api.WriteError(w, http.StatusBadGateway, "docker_operation_failed", "docker operation failed", nil)
	}
}

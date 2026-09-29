package runtimeimages

import (
	"context"
	"encoding/json"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"net/http"
	"time"
)

type Module struct{ service *Service }

func NewModule(s *Service) *Module { return &Module{service: s} }
func (m *Module) Name() string     { return "runtime-images" }
func (m *Module) RegisterRoutes(mux *http.ServeMux, mw api.ModuleMiddleware) {
	viewer := func(h http.HandlerFunc) http.Handler { return mw.Authenticate(mw.RequireRole(domain.RoleViewer, h)) }
	operator := func(h http.HandlerFunc) http.Handler { return mw.Authenticate(mw.RequireRole(domain.RoleOperator, h)) }
	mux.Handle("GET /api/v1/runtime-images/{runtime}", viewer(m.list))
	mux.Handle("GET /api/v1/runtime-images/{runtime}/versions", viewer(m.versions))
	mux.Handle("POST /api/v1/runtime-images/{runtime}/actions", operator(m.action))
}
func write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func fail(w http.ResponseWriter, err error) {
	api.WriteError(w, http.StatusBadRequest, "runtime_image_error", err.Error(), nil)
}
func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	v, err := m.service.List(ctx, r.PathValue("runtime"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, v)
}
func (m *Module) versions(w http.ResponseWriter, r *http.Request) {
	v, err := m.service.catalog.Versions(r.Context(), r.PathValue("runtime"))
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, v)
}
func (m *Module) action(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
		Action  string `json:"action"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		fail(w, err)
		return
	}
	var actor *string
	if u, ok := api.CurrentUser(r.Context()); ok {
		actor = &u.ID
	}
	remote := r.RemoteAddr
	job, err := m.service.Enqueue(r.Context(), r.PathValue("runtime"), body.Version, body.Action, actor, &remote)
	if err != nil {
		fail(w, err)
		return
	}
	write(w, http.StatusAccepted, job)
}

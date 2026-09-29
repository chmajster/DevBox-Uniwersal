package gitintegrations

import (
	"context"
	"encoding/json"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"net/http"
	"strconv"
	"time"
)

type Module struct {
	service *Service
	audit   *audit.Service
}

func NewModule(s *Service, a *audit.Service) *Module { return &Module{service: s, audit: a} }
func (m *Module) Name() string                       { return "git-integrations" }
func (m *Module) RegisterRoutes(mux *http.ServeMux, mw api.ModuleMiddleware) {
	secure := func(role domain.Role, h http.HandlerFunc) http.Handler {
		return mw.Authenticate(mw.RequireRole(role, h))
	}
	mux.Handle("GET /api/v1/git-integrations", secure(domain.RoleOperator, m.list))
	mux.Handle("POST /api/v1/git-integrations", secure(domain.RoleAdmin, m.create))
	mux.Handle("DELETE /api/v1/git-integrations/{id}", secure(domain.RoleAdmin, m.delete))
	for _, action := range []string{"test", "repositories", "branches", "overview"} {
		mux.Handle("GET /api/v1/git-integrations/{id}/"+action, secure(domain.RoleOperator, m.read(action)))
	}
}
func write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
func fail(w http.ResponseWriter, err error) {
	api.WriteError(w, http.StatusBadRequest, "git_integration_error", err.Error(), nil)
}
func (m *Module) record(r *http.Request, action, id string) {
	if m.audit == nil {
		return
	}
	var actor *string
	if u, ok := api.CurrentUser(r.Context()); ok {
		actor = &u.ID
	}
	remote := r.RemoteAddr
	_ = m.audit.Record(r.Context(), actor, "git.integration."+action, "git_integration", &id, nil, &remote)
}
func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	write(w, 200, items)
}
func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	var i Integration
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if err := d.Decode(&i); err != nil {
		fail(w, err)
		return
	}
	i, err := m.service.Create(r.Context(), i)
	if err != nil {
		fail(w, err)
		return
	}
	m.record(r, "create", i.ID)
	write(w, 201, i)
}
func (m *Module) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := m.service.Delete(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	m.record(r, "delete", id)
	write(w, 200, map[string]bool{"deleted": true})
}
func (m *Module) read(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		i, err := m.service.Get(ctx, r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			page, err = strconv.Atoi(raw)
			if err != nil {
				fail(w, err)
				return
			}
		}
		var result any
		switch action {
		case "test":
			result, err = m.service.Test(ctx, i)
		case "repositories":
			result, err = m.service.Repositories(ctx, i, page)
		case "branches":
			result, err = m.service.Branches(ctx, i, r.URL.Query().Get("repository"), page)
		case "overview":
			result, err = m.service.Overview(ctx, i, r.URL.Query().Get("repository"), r.URL.Query().Get("branch"))
		}
		if err != nil {
			fail(w, err)
			return
		}
		m.record(r, action, i.ID)
		write(w, 200, result)
	}
}

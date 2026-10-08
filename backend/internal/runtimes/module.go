package runtimes

import (
	"encoding/json"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"net/http"
)

type Module struct{}

func NewModule() *Module       { return &Module{} }
func (m *Module) Name() string { return "runtimes" }
func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(h http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, h))
	}
	mux.Handle("GET /api/v1/runtimes/catalog", viewer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRuntimeJSON(w, http.StatusOK, containerspec.RuntimeCatalog())
	})))
	mux.Handle("GET /api/v1/runtimes", viewer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRuntimeJSON(w, http.StatusOK, containerspec.RuntimeCatalog())
	})))
	mux.Handle("GET /api/v1/runtimes/{runtime}/modules", viewer(http.HandlerFunc(m.runtimeModules)))
}
func (m *Module) runtimeModules(w http.ResponseWriter, r *http.Request) {
	items, err := containerspec.Catalog(r.PathValue("runtime"))
	if err != nil {
		writeRuntimeError(w, http.StatusBadRequest, "invalid_runtime", err.Error())
		return
	}
	writeRuntimeJSON(w, http.StatusOK, items)
}

type runtimeEnvelope struct {
	Data  any              `json:"data,omitempty"`
	Error *runtimeAPIError `json:"error,omitempty"`
}

type runtimeAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeRuntimeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(runtimeEnvelope{Data: data})
}

func writeRuntimeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(runtimeEnvelope{Error: &runtimeAPIError{Code: code, Message: message}})
}

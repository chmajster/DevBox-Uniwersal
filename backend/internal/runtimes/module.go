package runtimes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

type Module struct {
	service *Service
}

func NewModule(registry Registry, resolver ProjectResolver, secretStore secrets.SecretStore) *Module {
	return NewModuleWithService(NewService(registry, resolver, secretStore))
}

func NewModuleWithService(service *Service) *Module {
	return &Module{service: service}
}

func (m *Module) Name() string { return "runtimes" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, handler))
	}
	operator := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, handler))
	}

	mux.Handle("GET /api/v1/runtimes", viewer(http.HandlerFunc(m.listRuntimes)))
	mux.Handle("GET /api/v1/runtimes/detect", viewer(http.HandlerFunc(m.detectRuntime)))
	mux.Handle("GET /api/v1/projects/{id}/runtime", viewer(http.HandlerFunc(m.projectRuntime)))
	mux.Handle("POST /api/v1/projects/{id}/runtime/validate", operator(http.HandlerFunc(m.validateRuntime)))
}

func (m *Module) listRuntimes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeRuntimeJSON(w, http.StatusOK, m.service.ListRuntimes(ctx))
}

func (m *Module) detectRuntime(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" {
		writeRuntimeError(w, http.StatusBadRequest, "invalid_project_id", "project_id query parameter is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := m.service.Detect(ctx, projectID)
	if err != nil {
		writeRuntimeServiceError(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, result)
}

func (m *Module) projectRuntime(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := m.service.ProjectRuntime(ctx, r.PathValue("id"))
	if err != nil {
		writeRuntimeServiceError(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, result)
}

func (m *Module) validateRuntime(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := m.service.Validate(ctx, r.PathValue("id"))
	if err != nil {
		writeRuntimeServiceError(w, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, result)
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

func writeRuntimeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrProjectNotFound):
		writeRuntimeError(w, http.StatusNotFound, "project_not_found", "project was not found")
	case errors.Is(err, ErrProjectWorkDirMissing):
		writeRuntimeError(w, http.StatusConflict, "project_workdir_missing", "project work directory is not configured")
	case errors.Is(err, ErrRuntimeNotDetected):
		writeRuntimeError(w, http.StatusUnprocessableEntity, "runtime_not_detected", "no supported runtime was detected")
	default:
		writeRuntimeError(w, http.StatusInternalServerError, "runtime_error", "runtime operation failed")
	}
}

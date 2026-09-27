package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Module struct {
	service       *Service
	ports         *PortManager
	health        *HealthChecker
	nginx         *NginxProvider
	audit         *audit.Service
	healthTimeout time.Duration
}

func NewModule(service *Service, ports *PortManager, health *HealthChecker, nginx *NginxProvider, auditService *audit.Service, healthTimeout time.Duration) *Module {
	return &Module{
		service: service, ports: ports, health: health, nginx: nginx,
		audit: auditService, healthTimeout: healthTimeout,
	}
}

func (m *Module) Name() string { return "networking" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(h http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, h))
	}
	operator := func(h http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, h))
	}
	admin := func(h http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleAdmin, h))
	}

	mux.Handle("GET /api/v1/ports", viewer(m.listPorts))
	mux.Handle("GET /api/v1/ports/{port}", viewer(m.inspectPort))
	mux.Handle("POST /api/v1/projects/{id}/port/allocate", operator(m.allocatePort))
	mux.Handle("DELETE /api/v1/ports/{port}", operator(m.releasePort))

	mux.Handle("GET /api/v1/domains", viewer(m.listDomains))
	mux.Handle("POST /api/v1/domains", operator(m.createDomain))
	mux.Handle("PATCH /api/v1/domains/{id}", operator(m.updateDomain))
	mux.Handle("DELETE /api/v1/domains/{id}", operator(m.deleteDomain))

	mux.Handle("GET /api/v1/proxy/status", viewer(m.proxyStatus))
	mux.Handle("POST /api/v1/proxy/test", operator(m.proxyTest))
	mux.Handle("POST /api/v1/proxy/reload", admin(m.proxyReload))
	mux.Handle("POST /api/v1/health-checks/run", operator(m.runHealthCheck))
}

func (m *Module) listPorts(w http.ResponseWriter, r *http.Request) {
	items, err := m.ports.List(r.Context())
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, items)
}

func (m *Module) inspectPort(w http.ResponseWriter, r *http.Request) {
	port, err := pathPort(r)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	item, err := m.ports.Inspect(r.Context(), port)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, item)
}

func (m *Module) allocatePort(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Purpose       string `json:"purpose"`
		PreferredPort *int   `json:"preferred_port"`
	}
	if err := decodeModuleJSON(r, &input); err != nil {
		writeModuleError(w, err)
		return
	}
	if strings.TrimSpace(input.Purpose) == "" {
		input.Purpose = "application"
	}
	projectID := r.PathValue("id")
	lease, err := m.ports.Reserve(r.Context(), projectID, input.Purpose, input.PreferredPort)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	record, err := m.ports.Inspect(r.Context(), lease.Port)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	if err := m.recordAudit(r, "port.allocate", "port", record.ID, map[string]any{
		"project_id": projectID, "port": record.Port, "purpose": record.Purpose,
	}); err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusCreated, record)
}

func (m *Module) releasePort(w http.ResponseWriter, r *http.Request) {
	port, err := pathPort(r)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	record, err := m.ports.Inspect(r.Context(), port)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	if err := m.ports.Release(r.Context(), port); err != nil {
		writeModuleError(w, err)
		return
	}
	if err := m.recordAudit(r, "port.release", "port", record.ID, map[string]any{"port": port}); err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, map[string]any{"port": port, "state": "released"})
}

func (m *Module) listDomains(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.ListDomains(r.Context())
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, items)
}

func (m *Module) createDomain(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID  string `json:"project_id"`
		Hostname   string `json:"hostname"`
		TargetPort int    `json:"target_port"`
	}
	if err := decodeModuleJSON(r, &input); err != nil {
		writeModuleError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := m.service.CreateDomain(ctx, input.ProjectID, input.Hostname, input.TargetPort)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	if err := m.recordAudit(r, "domain.create", "domain", result.Domain.ID, map[string]any{
		"hostname": result.Domain.Hostname, "target_port": result.Domain.TargetPort,
	}); err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusCreated, result)
}

func (m *Module) updateDomain(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Hostname   *string `json:"hostname"`
		TargetPort *int    `json:"target_port"`
	}
	if err := decodeModuleJSON(r, &input); err != nil {
		writeModuleError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := m.service.UpdateDomain(ctx, r.PathValue("id"), input.Hostname, input.TargetPort)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	if err := m.recordAudit(r, "domain.update", "domain", result.Domain.ID, map[string]any{
		"hostname": result.Domain.Hostname, "target_port": result.Domain.TargetPort,
	}); err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, result)
}

func (m *Module) deleteDomain(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	deleted, hostsResult, err := m.service.DeleteDomain(ctx, r.PathValue("id"))
	if err != nil {
		writeModuleError(w, err)
		return
	}
	if err := m.recordAudit(r, "domain.delete", "domain", deleted.ID, map[string]any{
		"hostname": deleted.Hostname, "target_port": deleted.TargetPort,
	}); err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, map[string]any{"domain": deleted, "hosts": hostsResult})
}

func (m *Module) proxyStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeModuleJSON(w, http.StatusOK, m.service.ProxyStatus(ctx))
}

func (m *Module) proxyTest(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Hostname   string `json:"hostname"`
		TargetPort int    `json:"target_port"`
	}
	if err := decodeModuleJSON(r, &input); err != nil {
		writeModuleError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if input.Hostname == "" && input.TargetPort == 0 {
		if err := m.nginx.Validate(ctx); err != nil {
			writeModuleError(w, err)
			return
		}
		writeModuleJSON(w, http.StatusOK, map[string]any{"valid": true, "scope": "active"})
		return
	}
	if input.Hostname == "" || input.TargetPort == 0 {
		writeModuleError(w, errors.New("invalid networking input: hostname and target_port must be provided together"))
		return
	}
	if err := m.service.TestRoute(ctx, input.Hostname, input.TargetPort); err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, map[string]any{"valid": true, "scope": "candidate"})
}

func (m *Module) proxyReload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := m.nginx.Reload(ctx); err != nil {
		writeModuleError(w, err)
		return
	}
	if err := m.recordAudit(r, "proxy.reload", "reverse_proxy", "nginx", nil); err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, map[string]any{"reloaded": true})
}

func (m *Module) runHealthCheck(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID      string `json:"project_id"`
		Type           string `json:"type"`
		Target         string `json:"target"`
		TimeoutSeconds *int   `json:"timeout_seconds"`
	}
	if err := decodeModuleJSON(r, &input); err != nil {
		writeModuleError(w, err)
		return
	}
	timeout := m.healthTimeout
	if input.TimeoutSeconds != nil {
		if *input.TimeoutSeconds < 1 || *input.TimeoutSeconds > 60 {
			writeModuleError(w, errors.New("invalid networking input: timeout_seconds must be between 1 and 60"))
			return
		}
		timeout = time.Duration(*input.TimeoutSeconds) * time.Second
	}
	result, err := m.health.RunAndStore(r.Context(), input.ProjectID, input.Type, input.Target, timeout)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	if err := m.recordAudit(r, "health.check", "health_check", input.ProjectID+":"+input.Type+":"+input.Target, map[string]any{
		"status": result.Status, "response_time_ms": result.ResponseTimeMS,
	}); err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusOK, result)
}

func (m *Module) recordAudit(r *http.Request, action, resourceType, resourceID string, metadata map[string]any) error {
	if m.audit == nil {
		return nil
	}
	var actor *string
	if user, ok := api.CurrentUser(r.Context()); ok {
		id := user.ID
		actor = &id
	}
	id := resourceID
	return m.audit.Record(r.Context(), actor, action, resourceType, &id, metadata, moduleRemoteIP(r))
}

func pathPort(r *http.Request) (int, error) {
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("invalid networking input: invalid port")
	}
	return port, nil
}

func moduleRemoteIP(r *http.Request) *string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return nil
	}
	return &host
}

func decodeModuleJSON(r *http.Request, target any) error {
	if r.Body == nil {
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(target)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return errors.New("invalid networking input: invalid JSON payload")
	}
	return nil
}

type moduleEnvelope struct {
	Data  any          `json:"data,omitempty"`
	Error *moduleError `json:"error,omitempty"`
}

type moduleError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func writeModuleJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(moduleEnvelope{Data: data})
}

func writeModuleError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "networking operation failed"
	var details any

	var privilege *PrivilegeError
	switch {
	case errors.As(err, &privilege):
		status = http.StatusConflict
		code = "privilege_required"
		message = privilege.Error()
		details = map[string]string{"path": privilege.Path, "instruction": privilege.Instruction}
	case errors.Is(err, ErrInvalidInput):
		status = http.StatusBadRequest
		code = "invalid_request"
		message = err.Error()
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
		message = err.Error()
	case errors.Is(err, ErrConflict), errors.Is(err, ErrPortInUse), errors.Is(err, ErrNoPorts):
		status = http.StatusConflict
		code = "conflict"
		message = err.Error()
	default:
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "invalid networking input") {
			status = http.StatusBadRequest
			code = "invalid_request"
			message = err.Error()
		} else if strings.Contains(lower, "nginx") {
			status = http.StatusServiceUnavailable
			code = "proxy_error"
			message = err.Error()
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(moduleEnvelope{Error: &moduleError{Code: code, Message: message, Details: details}})
}

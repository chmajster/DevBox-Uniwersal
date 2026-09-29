package databases

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type Module struct {
	service *Service
}

func NewModule(service *Service) *Module {
	return &Module{service: service}
}

func (m *Module) Name() string {
	return "databases"
}

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, handler))
	}
	operatorRead := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, handler))
	}
	operator := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, sameOrigin(handler)))
	}

	mux.Handle("GET /api/v1/mysql/status", viewer(http.HandlerFunc(m.mysqlStatus)))
	mux.Handle("POST /api/v1/mysql/{action}", operator(http.HandlerFunc(m.mysqlAction)))
	mux.Handle("GET /api/v1/databases", viewer(http.HandlerFunc(m.listDatabases)))
	mux.Handle("POST /api/v1/databases", operator(http.HandlerFunc(m.createDatabase)))
	mux.Handle("DELETE /api/v1/databases/{id}", operator(http.HandlerFunc(m.deleteDatabase)))

	mux.Handle("GET /api/v1/database-users", viewer(http.HandlerFunc(m.listUsers)))
	mux.Handle("POST /api/v1/database-users", operator(http.HandlerFunc(m.createUser)))
	mux.Handle("DELETE /api/v1/database-users/{id}", operator(http.HandlerFunc(m.deleteUser)))
	mux.Handle("POST /api/v1/database-users/{id}/password", operator(http.HandlerFunc(m.changePassword)))
	mux.Handle("POST /api/v1/database-users/{id}/grants", operator(http.HandlerFunc(m.changeGrants)))

	mux.Handle("GET /api/v1/projects/{id}/database-binding", viewer(http.HandlerFunc(m.getDatabaseBinding)))
	mux.Handle("PUT /api/v1/projects/{id}/database-binding", operator(http.HandlerFunc(m.updateDatabaseBinding)))
	mux.Handle("DELETE /api/v1/projects/{id}/database-binding", operator(http.HandlerFunc(m.deleteDatabaseBinding)))
	mux.Handle("POST /api/v1/projects/{id}/database-binding/test-network", operator(http.HandlerFunc(m.testDatabaseNetwork)))
	mux.Handle("POST /api/v1/projects/{id}/database-binding/test", operator(http.HandlerFunc(m.testDatabaseBinding)))
	mux.Handle("POST /api/v1/projects/{id}/database-binding/password", operator(http.HandlerFunc(m.rotateDatabasePassword)))
	mux.Handle("GET /api/v1/projects/{id}/database-binding/compose-services", viewer(http.HandlerFunc(m.composeServices)))
	mux.Handle("POST /api/v1/projects/{id}/database/provision", operator(http.HandlerFunc(m.provisionProject)))
	mux.Handle("POST /api/v1/databases/{id}/backup", operator(http.HandlerFunc(m.backupDatabase)))
	mux.Handle("GET /api/v1/databases/{id}/backups", viewer(http.HandlerFunc(m.listBackups)))
	mux.Handle("POST /api/v1/databases/{id}/restore", operator(http.HandlerFunc(m.restoreDatabase)))
	mux.Handle("GET /api/v1/database-backups/{id}/download", operatorRead(http.HandlerFunc(m.downloadBackup)))
	mux.Handle("DELETE /api/v1/database-backups/{id}", operator(http.HandlerFunc(m.deleteBackup)))

	mux.Handle("GET /api/v1/phpmyadmin/status", viewer(http.HandlerFunc(m.phpMyAdminStatus)))
	mux.Handle("POST /api/v1/phpmyadmin/{action}", operator(http.HandlerFunc(m.phpMyAdminAction)))
}

func (m *Module) mysqlStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.MySQLStatus(r.Context()))
}

func (m *Module) mysqlAction(w http.ResponseWriter, r *http.Request) {
	actor, remote := requestIdentity(r)
	job, err := m.service.QueueManagedMySQLAction(r.Context(), r.PathValue("action"), actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, job)
}

func (m *Module) listDatabases(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.ListDatabases(r.Context())
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (m *Module) createDatabase(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    string `json:"name"`
		Engine  string `json:"engine"`
		Charset string `json:"charset"`
	}
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	actor, remote := requestIdentity(r)
	item, err := m.service.CreateDatabase(r.Context(), strings.TrimSpace(input.Name), strings.TrimSpace(input.Engine), strings.TrimSpace(input.Charset), actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func (m *Module) deleteDatabase(w http.ResponseWriter, r *http.Request) {
	actor, remote := requestIdentity(r)
	if err := m.service.DeleteDatabase(r.Context(), r.PathValue("id"), actor, remote); err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (m *Module) listUsers(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.ListUsers(r.Context())
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (m *Module) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DatabaseID string   `json:"database_id"`
		Username   string   `json:"username"`
		Password   *string  `json:"password"`
		Privileges []string `json:"privileges"`
	}
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	actor, remote := requestIdentity(r)
	user, password, err := m.service.CreateUser(r.Context(), input.DatabaseID, strings.TrimSpace(input.Username), input.Password, input.Privileges, actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	credential, err := m.service.DatabaseUserConnection(r.Context(), user.ID, password)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusCreated, map[string]any{
		"user":       user,
		"credential": credential,
	})
}

func (m *Module) deleteUser(w http.ResponseWriter, r *http.Request) {
	actor, remote := requestIdentity(r)
	if err := m.service.DeleteUser(r.Context(), r.PathValue("id"), actor, remote); err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (m *Module) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password *string `json:"password"`
	}
	if r.ContentLength != 0 {
		if err := decodeBody(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
	}
	actor, remote := requestIdentity(r)
	userID := r.PathValue("id")
	password, err := m.service.ChangeUserPassword(r.Context(), userID, input.Password, actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	credential, err := m.service.DatabaseUserConnection(r.Context(), userID, password)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]any{"password": password, "credential": credential})
}

func (m *Module) changeGrants(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action     string   `json:"action"`
		Privileges []string `json:"privileges"`
	}
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	actor, remote := requestIdentity(r)
	user, err := m.service.ChangeGrants(r.Context(), r.PathValue("id"), input.Action, input.Privileges, actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, user)
}

func (m *Module) getDatabaseBinding(w http.ResponseWriter, r *http.Request) {
	item, err := m.service.GetDatabaseBinding(r.Context(), r.PathValue("id"))
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (m *Module) updateDatabaseBinding(w http.ResponseWriter, r *http.Request) {
	var input DatabaseBindingInput
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	actor, remote := requestIdentity(r)
	item, err := m.service.UpdateDatabaseBinding(r.Context(), r.PathValue("id"), input, actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (m *Module) deleteDatabaseBinding(w http.ResponseWriter, r *http.Request) {
	actor, remote := requestIdentity(r)
	if err := m.service.DeleteDatabaseBinding(r.Context(), r.PathValue("id"), actor, remote); err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, DatabaseBinding{ProjectID: r.PathValue("id"), Mode: DatabaseModeNone})
}

func (m *Module) testDatabaseBinding(w http.ResponseWriter, r *http.Request) {
	if err := m.service.TestApplicationConnection(r.Context(), r.PathValue("id")); err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "connected"})
}

func (m *Module) rotateDatabasePassword(w http.ResponseWriter, r *http.Request) {
	actor, remote := requestIdentity(r)
	if err := m.service.RotateProjectDatabasePassword(r.Context(), r.PathValue("id"), actor, remote); err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "rotated"})
}

func (m *Module) composeServices(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.ComposeServices(r.Context(), r.PathValue("id"))
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (m *Module) provisionProject(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Engine             string `json:"engine"`
		Charset            string `json:"charset"`
		ApplicationService string `json:"application_service"`
	}
	if r.ContentLength != 0 {
		if err := decodeBody(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
	}
	actor, remote := requestIdentity(r)
	result, err := m.service.ProvisionProject(r.Context(), r.PathValue("id"), input.Engine, input.Charset, actor, remote, input.ApplicationService)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusCreated, result)
}

func (m *Module) backupDatabase(w http.ResponseWriter, r *http.Request) {
	actor, remote := requestIdentity(r)
	job, backup, err := m.service.QueueBackup(r.Context(), r.PathValue("id"), actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, map[string]any{"job": job, "backup": backup})
}

func (m *Module) listBackups(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.ListBackups(r.Context(), r.PathValue("id"))
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (m *Module) restoreDatabase(w http.ResponseWriter, r *http.Request) {
	var input struct {
		BackupID string `json:"backup_id"`
	}
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	actor, remote := requestIdentity(r)
	job, err := m.service.QueueRestore(r.Context(), r.PathValue("id"), input.BackupID, actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, job)
}

func (m *Module) downloadBackup(w http.ResponseWriter, r *http.Request) {
	backup, path, err := m.service.BackupDownload(r.Context(), r.PathValue("id"))
	if err != nil {
		writeModuleError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/sql")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", backup.FileName))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}

func (m *Module) deleteBackup(w http.ResponseWriter, r *http.Request) {
	actor, remote := requestIdentity(r)
	if err := m.service.DeleteBackup(r.Context(), r.PathValue("id"), actor, remote); err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (m *Module) phpMyAdminStatus(w http.ResponseWriter, r *http.Request) {
	status, err := m.service.PHPMyAdminStatus(r.Context())
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusOK, status)
}

func (m *Module) phpMyAdminAction(w http.ResponseWriter, r *http.Request) {
	actor, remote := requestIdentity(r)
	status, err := m.service.QueuePHPMyAdminAction(r.Context(), r.PathValue("action"), actor, remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeData(w, http.StatusAccepted, status)
}

func requestIdentity(r *http.Request) (*string, *string) {
	var actor *string
	if user, ok := api.CurrentUser(r.Context()); ok {
		value := user.ID
		actor = &value
	}
	remoteValue := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteValue = host
	}
	if remoteValue == "" {
		return actor, nil
	}
	return actor, &remoteValue
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			writeError(w, http.StatusForbidden, "csrf_rejected", "cross-site mutation rejected", nil)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
				writeError(w, http.StatusForbidden, "csrf_rejected", "origin does not match request host", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeModuleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDatabaseUserInUse):
		writeError(w, http.StatusConflict, "database_user_in_use", err.Error(), nil)
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "database resource not found", nil)
	case errors.Is(err, ErrInvalidIdentifier), errors.Is(err, ErrInvalidPrivilege),
		strings.Contains(strings.ToLower(err.Error()), "invalid "),
		strings.Contains(err.Error(), "not selected"):
		writeError(w, http.StatusBadRequest, "validation_error", err.Error(), nil)
	case errors.Is(err, ErrSecretsUnavailable):
		writeError(w, http.StatusServiceUnavailable, "secret_store_unavailable", err.Error(), nil)
	case strings.Contains(err.Error(), "already has database"), strings.Contains(err.Error(), "UNIQUE constraint failed"):
		writeError(w, http.StatusConflict, "conflict", err.Error(), nil)
	case strings.Contains(err.Error(), "mysql"), strings.Contains(err.Error(), "database backup failed"), strings.Contains(err.Error(), "database restore failed"):
		writeError(w, http.StatusServiceUnavailable, "provider_unavailable", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
	}
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

func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(moduleEnvelope{Data: data})
}

func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(moduleEnvelope{Error: &moduleError{Code: code, Message: message, Details: details}})
}

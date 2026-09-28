package backups

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

const maxBackupUploadBytes int64 = 8 << 30

type Module struct {
	service *Service
	audit   *audit.Service
}

func NewModule(service *Service, auditService *audit.Service) *Module {
	return &Module{service: service, audit: auditService}
}

func (m *Module) Name() string { return "control-plane-backups" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	admin := func(handler http.Handler) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleAdmin, handler))
	}
	mux.Handle("GET /api/v1/system-backups", admin(http.HandlerFunc(m.list)))
	mux.Handle("POST /api/v1/system-backups", admin(http.HandlerFunc(m.create)))
	mux.Handle("POST /api/v1/system-backups/import", admin(http.HandlerFunc(m.importBackup)))
	mux.Handle("GET /api/v1/system-backups/{id}/download", admin(http.HandlerFunc(m.download)))
	mux.Handle("POST /api/v1/system-backups/{id}/restore", admin(http.HandlerFunc(m.restore)))
	mux.Handle("DELETE /api/v1/system-backups/{id}", admin(http.HandlerFunc(m.delete)))
}

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.List(r.Context())
	if err != nil {
		writeBackupError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, items)
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	actor := actorID(r)
	item, err := m.service.Create(r.Context(), actor)
	if err != nil {
		writeBackupError(w, err)
		return
	}
	m.record(r, "system.backup.create", item.ID, map[string]any{"file_name": item.FileName})
	api.WriteJSON(w, http.StatusAccepted, item)
}

func (m *Module) importBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid_backup_upload", "backup upload is invalid or too large", nil)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "backup_file_required", "multipart field 'file' is required", nil)
		return
	}
	defer file.Close()
	if header.Size > maxBackupUploadBytes {
		api.WriteError(w, http.StatusRequestEntityTooLarge, "backup_too_large", "backup exceeds upload limit", nil)
		return
	}
	actor := actorID(r)
	item, err := m.service.Import(r.Context(), actor, file)
	if err != nil {
		writeBackupError(w, err)
		return
	}
	m.record(r, "system.backup.import", item.ID, map[string]any{"source_name": header.Filename})
	api.WriteJSON(w, http.StatusAccepted, item)
}

func (m *Module) download(w http.ResponseWriter, r *http.Request) {
	item, path, err := m.service.DownloadPath(r.Context(), r.PathValue("id"))
	if err != nil {
		writeBackupError(w, err)
		return
	}
	m.record(r, "system.backup.download", item.ID, nil)
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", item.FileName))
	http.ServeFile(w, r, path)
}

func (m *Module) restore(w http.ResponseWriter, r *http.Request) {
	actor := actorID(r)
	item, err := m.service.Restore(r.Context(), r.PathValue("id"), actor)
	if err != nil {
		writeBackupError(w, err)
		return
	}
	m.record(r, "system.backup.restore", item.ID, map[string]any{"restart_required": true})
	api.WriteJSON(w, http.StatusAccepted, map[string]any{
		"backup":           item,
		"restart_required": true,
		"message":          "Restore will be staged by the job and applied on the next DevBox service start.",
	})
}

func (m *Module) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := m.service.Delete(r.Context(), id); err != nil {
		writeBackupError(w, err)
		return
	}
	m.record(r, "system.backup.delete", id, nil)
	api.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (m *Module) record(r *http.Request, action, id string, metadata map[string]any) {
	if m.audit == nil {
		return
	}
	_ = m.audit.Record(r.Context(), actorID(r), action, "system_backup", &id, metadata, nil)
}

func actorID(r *http.Request) *string {
	user, ok := api.CurrentUser(r.Context())
	if !ok {
		return nil
	}
	id := user.ID
	return &id
}

func writeBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		api.WriteError(w, http.StatusNotFound, "backup_not_found", "backup was not found", nil)
	case errors.Is(err, ErrBusy):
		api.WriteError(w, http.StatusConflict, "backup_busy", "backup is not in a state that allows this operation", nil)
	default:
		message := strings.TrimSpace(err.Error())
		if message == "" {
			message = "control-plane backup operation failed"
		}
		api.WriteError(w, http.StatusInternalServerError, "backup_error", message, nil)
	}
}

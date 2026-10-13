package projects

import (
	"encoding/json"
	"errors"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"net/http"
)

type Module struct {
	service *Service
	audit   *audit.Service
}

func NewModule(service *Service, auditService *audit.Service) *Module {
	return &Module{service: service, audit: auditService}
}
func (m *Module) fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	if errors.Is(err, ErrInvalidInput) {
		status = http.StatusBadRequest
		code = "invalid_request"
	}
	if errors.Is(err, ErrDirectoryAccess) {
		status = http.StatusForbidden
		code = "directory_access_denied"
	}
	writeAPIError(w, status, code, err.Error())
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

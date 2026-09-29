package proxy

import (
	"encoding/json"
	"encoding/pem"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"net/http"
)

type TLSModule struct{ service *TLSService }

func NewTLSModule(s *TLSService) *TLSModule { return &TLSModule{service: s} }
func (m *TLSModule) Name() string           { return "local-tls" }
func (m *TLSModule) RegisterRoutes(mux *http.ServeMux, mw api.ModuleMiddleware) {
	secure := func(role domain.Role, h http.HandlerFunc) http.Handler {
		return mw.Authenticate(mw.RequireRole(role, h))
	}
	mux.Handle("GET /api/v1/domains/{id}/tls", secure(domain.RoleViewer, m.status))
	mux.Handle("POST /api/v1/domains/{id}/tls", secure(domain.RoleAdmin, m.action))
	mux.Handle("GET /api/v1/proxy/tls/ca.crt", secure(domain.RoleViewer, m.ca))
}
func (m *TLSModule) status(w http.ResponseWriter, r *http.Request) {
	status, err := m.service.Status(r.Context(), r.PathValue("id"))
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, 200, status)
}
func (m *TLSModule) action(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action string `json:"action"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		writeModuleError(w, err)
		return
	}
	var actor *string
	if u, ok := api.CurrentUser(r.Context()); ok {
		actor = &u.ID
	}
	remote := r.RemoteAddr
	job, err := m.service.Enqueue(r.Context(), r.PathValue("id"), input.Action, actor, &remote)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	writeModuleJSON(w, http.StatusAccepted, job)
}
func (m *TLSModule) ca(w http.ResponseWriter, r *http.Request) {
	ca, _, err := m.service.authority(r.Context(), false)
	if err != nil {
		writeModuleError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="devbox-local-ca.crt"`)
	w.Header().Set("Cache-Control", "no-store")
	_ = pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})
}

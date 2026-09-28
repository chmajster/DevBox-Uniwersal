package projects

import (
	"errors"
	"net/http"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

func (m *Module) registerPortRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	mux.Handle("GET /api/v1/projects/{id}/ports/config", middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, http.HandlerFunc(m.portConfiguration))))
	mux.Handle("PUT /api/v1/projects/{id}/ports/config", middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, http.HandlerFunc(m.updatePortConfiguration))))
}

func (m *Module) portConfiguration(w http.ResponseWriter, r *http.Request) {
	config, err := m.service.PortConfiguration(r.Context(), r.PathValue("id"))
	if err != nil {
		m.fail(w, err)
		return
	}
	writeData(w, http.StatusOK, config)
}

func (m *Module) updatePortConfiguration(w http.ResponseWriter, r *http.Request) {
	settings := DefaultPortSettings()
	if err := decodeJSON(w, r, &settings); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	id := r.PathValue("id")
	config, err := m.service.UpdatePortConfiguration(r.Context(), id, settings)
	if errors.Is(err, ErrPortConfigurationBusy) {
		writeAPIError(w, http.StatusConflict, "deployment_in_progress", err.Error())
		return
	}
	if err != nil {
		m.fail(w, err)
		return
	}
	if err := m.audit.Record(r.Context(), actorID(r), "project.ports.update", "project", &id, map[string]any{
		"container_port": settings.ContainerPort, "host_port": settings.HostPort,
		"https_enabled": settings.HTTPSEnabled, "https_container_port": settings.HTTPSContainerPort,
		"https_host_port": settings.HTTPSHostPort, "compose_service": settings.ComposeService,
	}, nil); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "audit_failed", "port settings saved but audit persistence failed")
		return
	}
	writeData(w, http.StatusOK, config)
}

package plugins

import (
	"encoding/json"
	"net/http"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
)

func (m *Module) phpExtensions(w http.ResponseWriter, r *http.Request) {
	items, err := m.service.PHPExtensions(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "php_extensions_unavailable", err.Error())
		return
	}
	writeData(w, http.StatusOK, items)
}

func (m *Module) installPHPExtensions(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Extensions []string `json:"extensions"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Nieprawidłowa lista rozszerzeń PHP.")
		return
	}
	items, err := m.service.InstallPHPExtensions(r.Context(), input.Extensions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "php_extensions_install_failed", err.Error())
		return
	}
	if m.audit != nil {
		var actor *string
		if user, ok := api.CurrentUser(r.Context()); ok {
			id := user.ID
			actor = &id
		}
		_ = m.audit.Record(r.Context(), actor, "plugin.php_extensions.install", "plugin", nil, map[string]any{"extensions": input.Extensions}, nil)
	}
	writeData(w, http.StatusOK, items)
}

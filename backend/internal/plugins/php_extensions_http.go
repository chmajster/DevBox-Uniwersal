package plugins

import (
	"encoding/json"
	"net/http"
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
	m.enqueueInstall(w, r, "php-extensions", input.Extensions)
}

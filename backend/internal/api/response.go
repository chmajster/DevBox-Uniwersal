package api

import (
	"encoding/json"
	"net/http"
)

type envelope struct {
	Data  any            `json:"data,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
	Error *apiError      `json:"error,omitempty"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Data: data})
}

func writeJSONMeta(w http.ResponseWriter, status int, data any, meta map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Data: data, Meta: meta})
}

func writeError(w http.ResponseWriter, status int, code, message string, details any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: &apiError{Code: code, Message: message, Details: details}})
}

// WriteJSON exposes the common API success envelope to domain modules.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, data)
}

// WriteJSONMeta exposes the shared API envelope with pagination/metadata to modules.
func WriteJSONMeta(w http.ResponseWriter, status int, data any, meta map[string]any) {
	writeJSONMeta(w, status, data, meta)
}

// WriteError exposes the common API error envelope to domain modules.
func WriteError(w http.ResponseWriter, status int, code, message string, details any) {
	writeError(w, status, code, message, details)
}

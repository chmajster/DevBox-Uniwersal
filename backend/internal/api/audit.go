package api

import "net/http"

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	items, err := a.audit.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list audit events", nil)
		return
	}
	writeJSONMeta(w, http.StatusOK, items, map[string]any{"limit": limit, "offset": offset})
}

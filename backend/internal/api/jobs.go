package api

import (
	"errors"
	"net/http"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

func (a *API) listJobs(w http.ResponseWriter, r *http.Request) {
	limit, offset := pagination(r)
	items, err := a.jobs.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list jobs", nil)
		return
	}
	writeJSONMeta(w, http.StatusOK, items, map[string]any{"limit": limit, "offset": offset})
}

func (a *API) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := a.jobs.ByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "job not found", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load job", nil)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

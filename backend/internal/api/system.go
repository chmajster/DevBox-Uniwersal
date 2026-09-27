package api

import (
	"net/http"

	devsystem "github.com/chmajster/DevBox-Uniwersal/backend/internal/system"
)

func (a *API) systemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, devsystem.Current(a.version))
}

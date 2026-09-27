package api

import (
	"net/http"
	"time"

	devsystem "github.com/chmajster/DevBox-Uniwersal/backend/internal/system"
)

func (a *API) systemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, devsystem.Current(a.version))
}

func (a *API) systemComponents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 8*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, devsystem.DetectComponents(ctx))
}

func (a *API) systemPlatform(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, devsystem.DetectPlatform())
}

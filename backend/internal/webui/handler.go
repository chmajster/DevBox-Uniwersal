package webui

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Wrap serves a built frontend next to the API. API paths are always delegated
// untouched. Unknown non-API paths fall back to index.html for SPA routing.
func Wrap(api http.Handler, frontendDir string) http.Handler {
	if strings.TrimSpace(frontendDir) == "" {
		return api
	}
	index := filepath.Join(frontendDir, "index.html")
	if info, err := os.Stat(index); err != nil || info.IsDir() {
		return api
	}
	files := http.FileServer(http.Dir(frontendDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
			api.ServeHTTP(w, r)
			return
		}
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		candidate := filepath.Join(frontendDir, filepath.FromSlash(rel))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}

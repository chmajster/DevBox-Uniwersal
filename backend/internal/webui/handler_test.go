package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapServesSPAAndDelegatesAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>devbox-ui</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("api-ok")) })
	h := Wrap(api, dir)

	apiRec := httptest.NewRecorder()
	h.ServeHTTP(apiRec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if apiRec.Body.String() != "api-ok" {
		t.Fatalf("api not delegated: %s", apiRec.Body.String())
	}

	uiRec := httptest.NewRecorder()
	h.ServeHTTP(uiRec, httptest.NewRequest(http.MethodGet, "/projects/example", nil))
	if !strings.Contains(uiRec.Body.String(), "devbox-ui") {
		t.Fatalf("SPA fallback missing: %s", uiRec.Body.String())
	}
}

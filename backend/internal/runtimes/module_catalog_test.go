package runtimes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

func TestRuntimeCatalogRoute(t *testing.T) {
	mux := http.NewServeMux()
	authenticated, authorized := false, false
	NewModule().RegisterRoutes(mux, api.ModuleMiddleware{
		Authenticate: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { authenticated = true; next.ServeHTTP(w, r) })
		},
		RequireRole: func(role domain.Role, next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authorized = role == domain.RoleViewer
				next.ServeHTTP(w, r)
			})
		},
	})
	for _, runtime := range []string{"php", "node", "python", "go", "static"} {
		t.Run(runtime, func(t *testing.T) {
			authenticated, authorized = false, false
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/runtimes/"+runtime+"/modules", nil))
			if response.Code != http.StatusOK || !authenticated || !authorized {
				t.Fatalf("status=%d authenticated=%v authorized=%v", response.Code, authenticated, authorized)
			}
			var result struct {
				Data []containerspec.ModuleOption `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			expected, _ := containerspec.Catalog(runtime)
			if result.Data == nil || len(result.Data) != len(expected) {
				t.Fatalf("unexpected catalog: %s", response.Body.String())
			}
		})
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/runtimes/unsupported/modules", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", response.Code)
	}
	var result runtimeEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Error == nil || result.Error.Code != "invalid_runtime" {
		t.Fatalf("unexpected error: %s", response.Body.String())
	}
}

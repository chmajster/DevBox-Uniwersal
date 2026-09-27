package operations

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

func TestLogsModuleReturnsCapturedDevBoxMessage(t *testing.T) {
	registry := NewRegistry()
	source := NewRingLogSource("devbox", 10)
	source.Append("info", "backend ready", nil)
	if err := registry.Register(source); err != nil {
		t.Fatal(err)
	}

	module := NewModule(registry)
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, api.ModuleMiddleware{
		Authenticate: func(handler http.Handler) http.Handler { return handler },
		RequireRole: func(_ domain.Role, handler http.Handler) http.Handler {
			return handler
		},
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/logs?source=devbox", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "backend ready") {
		t.Fatalf("expected captured message, body=%s", response.Body.String())
	}
}

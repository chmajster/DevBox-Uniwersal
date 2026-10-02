package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type directoryAuditRepository struct{ events []domain.AuditEvent }

func (a *directoryAuditRepository) Append(_ context.Context, event domain.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}
func (a *directoryAuditRepository) List(_ context.Context, _, _ int) ([]domain.AuditEvent, error) {
	return a.events, nil
}

func directoryTestMux(t *testing.T, root string, allowed bool) (*http.ServeMux, *directoryAuditRepository) {
	t.Helper()
	repo := &directoryAuditRepository{}
	module := NewDirectoryModule(root, audit.NewService(repo))
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, api.ModuleMiddleware{
		Authenticate: func(next http.Handler) http.Handler { return next },
		RequireRole: func(role domain.Role, next http.Handler) http.Handler {
			if role != domain.RoleOperator {
				t.Fatalf("directory role = %s", role)
			}
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !allowed {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	})
	return mux, repo
}

func TestDirectoryModuleRegistersPickerWithoutProjectLifecycle(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "application"), 0o750); err != nil {
		t.Fatal(err)
	}
	mux, auditRepo := directoryTestMux(t, root, true)
	for _, path := range []string{"/api/v1/project-directories", "/api/v1/projects/directories", "/api/v1/project-directories?path=" + url.QueryEscape(root), "/api/v1/project-directories?suggest=" + url.QueryEscape(filepath.Join(root, "app"))} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["data"] == nil || !strings.Contains(response.Body.String(), "application") && strings.Contains(path, "?") {
			t.Fatalf("invalid listing: %s", response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil))
	if response.Code != http.StatusNotFound {
		t.Fatal("directory module must not expose legacy Project lifecycle")
	}
	if len(auditRepo.events) != 3 || auditRepo.events[0].Action != "project.directory_browse" {
		t.Fatalf("audit = %+v", auditRepo.events)
	}
}

func TestDirectoryModuleCreatesDirectoryAndEnforcesRoots(t *testing.T) {
	root := t.TempDir()
	mux, auditRepo := directoryTestMux(t, root, true)
	body, err := json.Marshal(map[string]string{"parent": root, "name": "new-app"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/project-directories", strings.NewReader(string(body))))
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	if info, err := os.Stat(filepath.Join(root, "new-app")); err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
	if len(auditRepo.events) != 1 || auditRepo.events[0].Action != "project.directory_create" {
		t.Fatalf("audit = %+v", auditRepo.events)
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/project-directories?path="+url.QueryEscape(t.TempDir()), nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("outside root: %d %s", response.Code, response.Body.String())
	}
}

func TestDirectoryModuleRequiresOperatorForAllRoutes(t *testing.T) {
	mux, auditRepo := directoryTestMux(t, t.TempDir(), false)
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/project-directories"},
		{http.MethodGet, "/api/v1/project-directories?suggest=app"},
		{http.MethodGet, "/api/v1/projects/directories"},
		{http.MethodPost, "/api/v1/project-directories"},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s: %d", request.method, request.path, response.Code)
		}
	}
	if len(auditRepo.events) != 0 {
		t.Fatal("denied requests reached directory handlers")
	}
}

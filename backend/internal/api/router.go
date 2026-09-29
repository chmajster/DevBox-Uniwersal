package api

import (
	"database/sql"
	"net/http"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/auth"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

const sessionCookieName = "devbox_session"

type API struct {
	db           *sql.DB
	auth         *auth.Service
	audit        *audit.Service
	jobs         repository.JobRepository
	version      string
	cookieSecure bool
	authDisabled bool
}

type Dependencies struct {
	DB               *sql.DB
	Auth             *auth.Service
	Audit            *audit.Service
	Jobs             repository.JobRepository
	Version          string
	CookieSecure     bool
	AuthDisabled     bool
	Modules          []Module
	MaintenanceCheck func() error
}

func New(deps Dependencies) http.Handler {
	a := &API{db: deps.DB, auth: deps.Auth, audit: deps.Audit, jobs: deps.Jobs, version: deps.Version, cookieSecure: deps.CookieSecure, authDisabled: deps.AuthDisabled}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", a.health)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)

	mux.Handle("POST /api/v1/auth/logout", a.authenticate(http.HandlerFunc(a.logout)))
	mux.Handle("GET /api/v1/auth/me", a.authenticate(http.HandlerFunc(a.me)))
	mux.Handle("GET /api/v1/users", a.authenticate(requireRole(domain.RoleAdmin, http.HandlerFunc(a.listUsers))))
	mux.Handle("POST /api/v1/users", a.authenticate(requireRole(domain.RoleAdmin, http.HandlerFunc(a.createUser))))
	mux.Handle("PATCH /api/v1/users/{id}", a.authenticate(requireRole(domain.RoleAdmin, http.HandlerFunc(a.updateUser))))
	mux.Handle("POST /api/v1/users/{id}/password", a.authenticate(requireRole(domain.RoleAdmin, http.HandlerFunc(a.changeUserPassword))))
	mux.Handle("POST /api/v1/users/{id}/sessions/revoke", a.authenticate(requireRole(domain.RoleAdmin, http.HandlerFunc(a.revokeUserSessions))))
	mux.Handle("DELETE /api/v1/users/{id}", a.authenticate(requireRole(domain.RoleAdmin, http.HandlerFunc(a.deleteUser))))
	mux.Handle("GET /api/v1/system/info", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.systemInfo))))
	mux.Handle("GET /api/v1/system/platform", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.systemPlatform))))
	mux.Handle("GET /api/v1/system/components", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.systemComponents))))
	mux.Handle("GET /api/v1/jobs", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.listJobs))))
	mux.Handle("GET /api/v1/jobs/{id}", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.getJob))))
	mux.Handle("GET /api/v1/audit", a.authenticate(requireRole(domain.RoleOperator, http.HandlerFunc(a.listAudit))))
	mux.Handle("GET /api/v1/openapi.json", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.openAPI))))
	mux.Handle("GET /api/v1/docs", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.apiDocs))))

	middleware := ModuleMiddleware{Authenticate: a.authenticate, RequireRole: requireRole}
	for _, module := range deps.Modules {
		module.RegisterRoutes(mux, middleware)
	}

	return withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if deps.MaintenanceCheck != nil && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && r.URL.Path != "/api/v1/auth/login" && r.URL.Path != "/api/v1/auth/logout" {
			if err := deps.MaintenanceCheck(); err != nil {
				w.Header().Set("Retry-After", "30")
				writeError(w, http.StatusServiceUnavailable, "maintenance", "Control-plane update in progress; mutations are temporarily paused", nil)
				return
			}
		}
		mux.ServeHTTP(w, r)
	}))
}

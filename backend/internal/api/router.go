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
}

type Dependencies struct {
	DB           *sql.DB
	Auth         *auth.Service
	Audit        *audit.Service
	Jobs         repository.JobRepository
	Version      string
	CookieSecure bool
	Modules      []Module
}

func New(deps Dependencies) http.Handler {
	a := &API{db: deps.DB, auth: deps.Auth, audit: deps.Audit, jobs: deps.Jobs, version: deps.Version, cookieSecure: deps.CookieSecure}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", a.health)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)

	mux.Handle("POST /api/v1/auth/logout", a.authenticate(http.HandlerFunc(a.logout)))
	mux.Handle("GET /api/v1/auth/me", a.authenticate(http.HandlerFunc(a.me)))
	mux.Handle("GET /api/v1/system/info", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.systemInfo))))
	mux.Handle("GET /api/v1/jobs", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.listJobs))))
	mux.Handle("GET /api/v1/jobs/{id}", a.authenticate(requireRole(domain.RoleViewer, http.HandlerFunc(a.getJob))))
	mux.Handle("GET /api/v1/audit", a.authenticate(requireRole(domain.RoleOperator, http.HandlerFunc(a.listAudit))))

	middleware := ModuleMiddleware{Authenticate: a.authenticate, RequireRole: requireRole}
	for _, module := range deps.Modules {
		module.RegisterRoutes(mux, middleware)
	}

	return withSecurityHeaders(mux)
}

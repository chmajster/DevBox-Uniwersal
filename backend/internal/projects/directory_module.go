package projects

import (
	"net/http"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

// DirectoryModule exposes the source picker independently of the legacy
// Project lifecycle, reusing its allowlisted browser and audited handlers.
type DirectoryModule struct {
	module *Module
}

func NewDirectoryModule(projectsRoot string, auditService *audit.Service, browseRoots ...string) *DirectoryModule {
	service := &Service{directoryBrowseRoots: normalizeDirectoryBrowseRoots(projectsRoot, browseRoots)}
	return &DirectoryModule{module: NewModule(service, auditService)}
}

func (m *DirectoryModule) Name() string { return "project-directories" }

func (m *DirectoryModule) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	m.module.registerDirectoryRoutes(mux, middleware)
}

func (m *Module) registerDirectoryRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	secure := func(handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleOperator, handler))
	}
	mux.Handle("GET /api/v1/project-directories", secure(m.browseDirectories))
	mux.Handle("POST /api/v1/project-directories", secure(m.createDirectory))
	mux.Handle("GET /api/v1/projects/directories", secure(m.browseDirectories)) // legacy alias
}

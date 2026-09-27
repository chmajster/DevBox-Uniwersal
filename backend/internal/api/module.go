package api

import (
	"net/http"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

// Module is the extension boundary for domain-specific HTTP APIs.
// Agents should own their package and expose a Module rather than adding handlers to a monolithic controller.
type Module interface {
	Name() string
	RegisterRoutes(mux *http.ServeMux, middleware ModuleMiddleware)
}

type ModuleMiddleware struct {
	Authenticate func(http.Handler) http.Handler
	RequireRole  func(domain.Role, http.Handler) http.Handler
}

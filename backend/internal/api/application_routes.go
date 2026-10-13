package api

// The index retains legacy extension contracts for their owning modules, while
// explicitly documenting the active Application workspace API (ADR 012).
func init() {
	documentedRoutes = append(documentedRoutes,
		documentedRoute{"GET", "/api/v1/applications", "List applications and observed status", "viewer"},
		documentedRoute{"POST", "/api/v1/applications", "Create application", "operator"},
		documentedRoute{"POST", "/api/v1/applications/detect", "Analyze application source", "operator"},
		documentedRoute{"GET", "/api/v1/applications/{id}", "Application detail", "viewer"},
		documentedRoute{"GET", "/api/v1/applications/{id}/php-modules", "Read PHP modules installed in the primary application container", "viewer"},
		documentedRoute{"PATCH", "/api/v1/applications/{id}", "Update public application configuration", "operator"},
		documentedRoute{"DELETE", "/api/v1/applications/{id}", "Queue safe application removal", "admin"},
		documentedRoute{"GET", "/api/v1/applications/{id}/secrets", "List secret names, never values", "viewer"},
		documentedRoute{"PUT", "/api/v1/applications/{id}/secrets/{name}", "Encrypt application secret", "operator"},
		documentedRoute{"DELETE", "/api/v1/applications/{id}/secrets/{name}", "Delete application secret", "operator"},
		documentedRoute{"POST", "/api/v1/applications/{id}/jobs/{jobID}/cancel", "Cancel owned application job", "operator"},
		documentedRoute{"POST", "/api/v1/applications/{id}/jobs/{jobID}/retry", "Retry owned job (removal requires Admin)", "operator"},
	)
	for _, action := range []string{"deploy", "start", "stop", "restart", "reconcile"} {
		documentedRoutes = append(documentedRoutes, documentedRoute{"POST", "/api/v1/applications/{id}/" + action, "Queue application " + action, "operator"})
	}
	for _, view := range []string{"state", "workloads", "endpoints", "deployments", "events", "logs"} {
		documentedRoutes = append(documentedRoutes, documentedRoute{"GET", "/api/v1/applications/{id}/" + view, "Read application " + view, "viewer"})
	}
}

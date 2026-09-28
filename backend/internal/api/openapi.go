package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
)

type documentedRoute struct {
	Method  string
	Path    string
	Summary string
	Role    string
}

var documentedRoutes = []documentedRoute{
	{"GET", "/api/v1/health", "Control-plane health", ""},
	{"POST", "/api/v1/auth/login", "Create authenticated session", ""},
	{"POST", "/api/v1/auth/logout", "End authenticated session", "viewer"},
	{"GET", "/api/v1/auth/me", "Current authenticated user", "viewer"},
	{"GET", "/api/v1/system/info", "System information", "viewer"},
	{"GET", "/api/v1/system/platform", "Host platform details", "viewer"},
	{"GET", "/api/v1/system/components", "Installed component status", "viewer"},
	{"GET", "/api/v1/credentials", "List central credentials", "operator"},
	{"POST", "/api/v1/credentials", "Create central credential", "operator"},
	{"PATCH", "/api/v1/credentials/{id}", "Update central credential", "operator"},
	{"DELETE", "/api/v1/credentials/{id}", "Delete central credential", "operator"},
	{"GET", "/api/v1/update/status", "Check DevBox Git update status", "admin"},
	{"POST", "/api/v1/update/apply", "Start DevBox Git update", "admin"},
	{"GET", "/api/v1/script-apps", "List URL/script-installed applications", "viewer"},
	{"POST", "/api/v1/script-apps", "Create and optionally install a URL/script application", "admin"},
	{"GET", "/api/v1/script-apps/{id}", "Get URL/script application", "viewer"},
	{"GET", "/api/v1/script-apps/{id}/logs", "Read managed application logs", "viewer"},
	{"POST", "/api/v1/script-apps/{id}/refresh", "Refresh managed application status", "viewer"},
	{"POST", "/api/v1/script-apps/{id}/{action}", "Install, update, uninstall or control managed application", "admin"},
	{"DELETE", "/api/v1/script-apps/{id}", "Delete uninstalled application record", "admin"},
	{"GET", "/api/v1/projects", "List projects", "viewer"},
	{"POST", "/api/v1/projects", "Create project", "operator"},
	{"POST", "/api/v1/projects/import", "Import local project", "operator"},
	{"GET", "/api/v1/project-directories", "Browse allowed local project directories", "operator"},
	{"GET", "/api/v1/projects/{id}", "Get project", "viewer"},
	{"PATCH", "/api/v1/projects/{id}", "Update project", "operator"},
	{"DELETE", "/api/v1/projects/{id}", "Delete project", "admin"},
	{"POST", "/api/v1/projects/{id}/archive", "Archive project", "operator"},
	{"GET", "/api/v1/projects/{id}/git", "Project Git state", "viewer"},
	{"POST", "/api/v1/projects/{id}/git/fetch", "Fetch project repository", "operator"},
	{"POST", "/api/v1/projects/{id}/git/pull", "Pull project repository", "operator"},
	{"POST", "/api/v1/projects/{id}/git/checkout", "Checkout project branch", "operator"},
	{"POST", "/api/v1/projects/{id}/deploy", "Deploy project", "operator"},
	{"GET", "/api/v1/projects/{id}/deployments", "Project deployment history", "viewer"},
	{"GET", "/api/v1/projects/{id}/ports/config", "Get project Docker port publishing configuration", "viewer"},
	{"PUT", "/api/v1/projects/{id}/ports/config", "Update project Docker port publishing configuration", "operator"},
	{"GET", "/api/v1/runtimes", "Runtime inventory", "viewer"},
	{"GET", "/api/v1/runtimes/detect", "Detect project runtime", "viewer"},
	{"GET", "/api/v1/projects/{id}/runtime", "Project runtime details", "viewer"},
	{"GET", "/api/v1/projects/{id}/runtime/config", "Project managed runtime container configuration", "viewer"},
	{"PUT", "/api/v1/projects/{id}/runtime/config", "Update project managed runtime container configuration", "operator"},
	{"POST", "/api/v1/projects/{id}/runtime/rebuild", "Rebuild project managed runtime container", "operator"},
	{"GET", "/api/v1/runtimes/{runtime}/modules", "List allowlisted modules for managed runtime", "viewer"},
	{"POST", "/api/v1/projects/{id}/runtime/validate", "Validate project runtime", "operator"},
	{"GET", "/api/v1/docker/status", "Docker engine status", "viewer"},
	{"GET", "/api/v1/docker/containers", "List Docker containers", "viewer"},
	{"GET", "/api/v1/docker/containers/{id}", "Inspect Docker container", "viewer"},
	{"POST", "/api/v1/docker/containers/{id}/start", "Start Docker container", "operator"},
	{"POST", "/api/v1/docker/containers/{id}/stop", "Stop Docker container", "operator"},
	{"POST", "/api/v1/docker/containers/{id}/restart", "Restart Docker container", "operator"},
	{"DELETE", "/api/v1/docker/containers/{id}", "Remove Docker container", "admin"},
	{"GET", "/api/v1/docker/containers/{id}/logs", "Docker container logs", "viewer"},
	{"POST", "/api/v1/docker/containers/{id}/exec", "Run allowlisted container command", "operator"},
	{"GET", "/api/v1/docker/images", "List Docker images", "viewer"},
	{"GET", "/api/v1/docker/volumes", "List Docker volumes", "viewer"},
	{"GET", "/api/v1/docker/networks", "List Docker networks", "viewer"},
	{"GET", "/api/v1/docker/compose/projects", "List Compose projects", "viewer"},
	{"GET", "/api/v1/docker/compose/projects/{name}/ps", "Compose project processes", "viewer"},
	{"GET", "/api/v1/docker/compose/projects/{name}/logs", "Compose project logs", "viewer"},
	{"GET", "/api/v1/mysql/status", "MySQL status", "viewer"},
	{"POST", "/api/v1/mysql/{action}", "Install, start, stop or restart managed MySQL", "operator"},
	{"GET", "/api/v1/databases", "List databases", "viewer"},
	{"POST", "/api/v1/databases", "Create database", "operator"},
	{"DELETE", "/api/v1/databases/{id}", "Delete database", "operator"},
	{"GET", "/api/v1/database-users", "List database users", "viewer"},
	{"POST", "/api/v1/database-users", "Create database user", "operator"},
	{"DELETE", "/api/v1/database-users/{id}", "Delete database user", "operator"},
	{"POST", "/api/v1/database-users/{id}/password", "Change database password", "operator"},
	{"POST", "/api/v1/database-users/{id}/grants", "Change database grants", "operator"},
	{"GET", "/api/v1/projects/{id}/database-binding", "Get project database binding", "viewer"},
	{"PUT", "/api/v1/projects/{id}/database-binding", "Create or update project database binding", "operator"},
	{"DELETE", "/api/v1/projects/{id}/database-binding", "Remove project database binding", "operator"},
	{"POST", "/api/v1/projects/{id}/database-binding/test", "Run a real database connectivity test", "operator"},
	{"POST", "/api/v1/projects/{id}/database-binding/password", "Rotate managed project database password", "operator"},
	{"GET", "/api/v1/projects/{id}/database-binding/compose-services", "List project Compose services for database binding", "viewer"},
	{"POST", "/api/v1/projects/{id}/database/provision", "Provision project managed database", "operator"},
	{"POST", "/api/v1/databases/{id}/backup", "Backup database", "operator"},
	{"GET", "/api/v1/databases/{id}/backups", "List database backups", "viewer"},
	{"POST", "/api/v1/databases/{id}/restore", "Restore database", "operator"},
	{"GET", "/api/v1/database-backups/{id}/download", "Download database backup", "operator"},
	{"DELETE", "/api/v1/database-backups/{id}", "Delete database backup", "operator"},
	{"GET", "/api/v1/phpmyadmin/status", "phpMyAdmin status", "viewer"},
	{"POST", "/api/v1/phpmyadmin/{action}", "Manage phpMyAdmin", "operator"},
	{"GET", "/api/v1/ports", "List ports", "viewer"},
	{"GET", "/api/v1/ports/{port}", "Inspect port", "viewer"},
	{"POST", "/api/v1/projects/{id}/port/allocate", "Allocate project port", "operator"},
	{"DELETE", "/api/v1/ports/{port}", "Release port", "operator"},
	{"GET", "/api/v1/domains", "List domains", "viewer"},
	{"POST", "/api/v1/domains", "Create domain", "operator"},
	{"PATCH", "/api/v1/domains/{id}", "Update domain", "operator"},
	{"DELETE", "/api/v1/domains/{id}", "Delete domain", "operator"},
	{"GET", "/api/v1/proxy/status", "Reverse proxy status", "viewer"},
	{"POST", "/api/v1/proxy/test", "Test reverse proxy configuration", "operator"},
	{"POST", "/api/v1/proxy/reload", "Reload reverse proxy", "admin"},
	{"POST", "/api/v1/health-checks/run", "Run ad-hoc networking health check", "operator"},
	{"GET", "/api/v1/health-checks", "List application health checks", "viewer"},
	{"GET", "/api/v1/health-checks/history", "Application health history", "viewer"},
	{"POST", "/api/v1/projects/{id}/health-check", "Run project health check", "operator"},
	{"GET", "/api/v1/monitoring/snapshot", "Host monitoring snapshot", "viewer"},
	{"GET", "/api/v1/monitoring/stream", "Host monitoring SSE stream", "viewer"},
	{"GET", "/api/v1/logs/sources", "List log sources", "viewer"},
	{"GET", "/api/v1/logs", "Query central logs", "viewer"},
	{"GET", "/api/v1/logs/stream", "Central live log SSE stream", "viewer"},
	{"GET", "/api/v1/logs/export", "Export filtered central logs", "viewer"},
	{"GET", "/api/v1/jobs", "List jobs", "viewer"},
	{"GET", "/api/v1/jobs/{id}", "Get job", "viewer"},
	{"GET", "/api/v1/jobs/{id}/logs", "Job logs", "viewer"},
	{"GET", "/api/v1/jobs/{id}/logs/stream", "Job live log stream", "viewer"},
	{"GET", "/api/v1/audit", "Audit events", "operator"},
	{"GET", "/api/v1/system-backups", "List control-plane backups", "admin"},
	{"POST", "/api/v1/system-backups", "Create control-plane backup", "admin"},
	{"POST", "/api/v1/system-backups/import", "Import control-plane backup", "admin"},
	{"GET", "/api/v1/system-backups/{id}/download", "Download control-plane backup", "admin"},
	{"POST", "/api/v1/system-backups/{id}/restore", "Stage control-plane restore", "admin"},
	{"DELETE", "/api/v1/system-backups/{id}", "Delete control-plane backup", "admin"},
	{"GET", "/api/v1/openapi.json", "OpenAPI 3.1 contract", "viewer"},
	{"GET", "/api/v1/docs", "Human-readable API index", "viewer"},
}

func (a *API) openAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.oai.openapi+json;version=3.1")
	_ = json.NewEncoder(w).Encode(buildOpenAPISpec(a.version))
}

func buildOpenAPISpec(version string) map[string]any {
	paths := map[string]any{}
	for _, route := range documentedRoutes {
		pathItem, _ := paths[route.Path].(map[string]any)
		if pathItem == nil {
			pathItem = map[string]any{}
			paths[route.Path] = pathItem
		}
		operation := map[string]any{
			"summary":     route.Summary,
			"operationId": operationID(route),
			"responses": map[string]any{
				"200": map[string]any{"description": "Successful response"},
				"400": map[string]any{"description": "Invalid request"},
				"401": map[string]any{"description": "Authentication required"},
				"403": map[string]any{"description": "Insufficient permissions"},
			},
		}
		if route.Role != "" {
			operation["security"] = []map[string][]string{{"cookieAuth": []string{}}}
			operation["x-devbox-min-role"] = route.Role
		}
		if parameters := openAPIPathParameters(route.Path); len(parameters) > 0 {
			operation["parameters"] = parameters
		}
		pathItem[strings.ToLower(route.Method)] = operation
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "DevBox Universal API",
			"version":     version,
			"description": "Versioned control-plane API. JSON endpoints use the DevBox data/error envelope unless they explicitly return a stream or file.",
		},
		"servers": []map[string]string{{"url": "/"}},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"cookieAuth": map[string]any{
					"type": "apiKey",
					"in":   "cookie",
					"name": "devbox_session",
				},
			},
		},
	}
}

func operationID(route documentedRoute) string {
	replacer := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_")
	return strings.ToLower(route.Method) + replacer.Replace(route.Path)
}

func openAPIPathParameters(path string) []map[string]any {
	parameters := make([]map[string]any, 0)
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			break
		}
		rest := path[start:]
		endOffset := strings.Index(rest, "}")
		if endOffset < 0 {
			break
		}
		name := rest[1:endOffset]
		parameters = append(parameters, map[string]any{
			"name":     name,
			"in":       "path",
			"required": true,
			"schema":   map[string]string{"type": "string"},
		})
		path = rest[endOffset+1:]
	}
	return parameters
}

func (a *API) apiDocs(w http.ResponseWriter, _ *http.Request) {
	routes := append([]documentedRoute(nil), documentedRoutes...)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Path < routes[j].Path
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, "<!doctype html><html><head><meta charset=\"utf-8\"><title>DevBox Universal API</title></head><body><h1>DevBox Universal API</h1><p><a href=\"/api/v1/openapi.json\">OpenAPI 3.1 JSON</a></p><table><thead><tr><th>Method</th><th>Path</th><th>Minimum role</th><th>Description</th></tr></thead><tbody>")
	for _, route := range routes {
		role := route.Role
		if role == "" {
			role = "public"
		}
		_, _ = fmt.Fprintf(w, "<tr><td><code>%s</code></td><td><code>%s</code></td><td>%s</td><td>%s</td></tr>",
			html.EscapeString(route.Method), html.EscapeString(route.Path), html.EscapeString(role), html.EscapeString(route.Summary))
	}
	_, _ = fmt.Fprint(w, "</tbody></table></body></html>")
}

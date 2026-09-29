package api

import "testing"

func TestOpenAPISpecContainsCoreAndModuleRoutes(t *testing.T) {
	spec := buildOpenAPISpec("test")
	if spec["openapi"] != "3.1.0" {
		t.Fatalf("openapi = %#v", spec["openapi"])
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("paths missing")
	}
	for _, path := range []string{
		"/api/v1/users",
		"/api/v1/update/progress",
		"/api/v1/projects",
		"/api/v1/projects/{id}/ports/config",
		"/api/v1/docker/containers",
		"/api/v1/plugins/mysql/status",
		"/api/v1/plugins/mysql/install",
		"/api/v1/plugins/mysql/{action}",
		"/api/v1/plugins/postgresql/status",
		"/api/v1/plugins/postgresql/install",
		"/api/v1/plugins/postgresql/{action}",
		"/api/v1/plugins/databases/host",
		"/api/v1/health-checks",
		"/api/v1/logs/export",
	} {
		if _, exists := paths[path]; !exists {
			t.Fatalf("missing documented path %s", path)
		}
	}
}

func TestOpenAPIPortConfigurationMethodsAndRoles(t *testing.T) {
	spec := buildOpenAPISpec("test")
	paths := spec["paths"].(map[string]any)
	item, ok := paths["/api/v1/projects/{id}/ports/config"].(map[string]any)
	if !ok {
		t.Fatal("project port configuration path missing")
	}
	get, ok := item["get"].(map[string]any)
	if !ok || get["x-devbox-min-role"] != "viewer" {
		t.Fatalf("GET port configuration role = %#v", get["x-devbox-min-role"])
	}
	put, ok := item["put"].(map[string]any)
	if !ok || put["x-devbox-min-role"] != "operator" {
		t.Fatalf("PUT port configuration role = %#v", put["x-devbox-min-role"])
	}
}

func TestOpenAPIDatabasePluginRoles(t *testing.T) {
	spec := buildOpenAPISpec("test")
	paths := spec["paths"].(map[string]any)

	statusPath, ok := paths["/api/v1/plugins/mysql/status"].(map[string]any)
	if !ok {
		t.Fatal("Docker MySQL status path missing")
	}
	get, ok := statusPath["get"].(map[string]any)
	if !ok || get["x-devbox-min-role"] != "viewer" {
		t.Fatalf("GET Docker MySQL status role = %#v", get["x-devbox-min-role"])
	}

	installPath, ok := paths["/api/v1/plugins/mysql/install"].(map[string]any)
	if !ok {
		t.Fatal("Docker MySQL install path missing")
	}
	post, ok := installPath["post"].(map[string]any)
	if !ok || post["x-devbox-min-role"] != "admin" {
		t.Fatalf("POST Docker MySQL install role = %#v", post["x-devbox-min-role"])
	}

	actionPath, ok := paths["/api/v1/plugins/mysql/{action}"].(map[string]any)
	if !ok {
		t.Fatal("Docker MySQL lifecycle path missing")
	}
	post, ok = actionPath["post"].(map[string]any)
	if !ok || post["x-devbox-min-role"] != "admin" {
		t.Fatalf("POST Docker MySQL lifecycle role = %#v", post["x-devbox-min-role"])
	}

	postgresActionPath, ok := paths["/api/v1/plugins/postgresql/{action}"].(map[string]any)
	if !ok {
		t.Fatal("Docker PostgreSQL lifecycle path missing")
	}
	post, ok = postgresActionPath["post"].(map[string]any)
	if !ok || post["x-devbox-min-role"] != "admin" {
		t.Fatalf("POST Docker PostgreSQL lifecycle role = %#v", post["x-devbox-min-role"])
	}
}

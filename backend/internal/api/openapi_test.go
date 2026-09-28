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
		"/api/v1/projects",
		"/api/v1/docker/containers",
		"/api/v1/health-checks",
		"/api/v1/logs/export",
	} {
		if _, exists := paths[path]; !exists {
			t.Fatalf("missing documented path %s", path)
		}
	}
}

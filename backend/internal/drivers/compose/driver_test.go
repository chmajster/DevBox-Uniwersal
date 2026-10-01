package compose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	dockerapi "github.com/chmajster/DevBox-Uniwersal/backend/internal/docker"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type testEngine struct {
	engine
	services  []dockerapi.ComposeApplicationService
	discovery providers.ComposePortDiscovery
}

func (e *testEngine) ComposeApplicationServices(context.Context, string, string) ([]dockerapi.ComposeApplicationService, error) {
	return e.services, nil
}
func (e *testEngine) DiscoverComposeApplicationPorts(context.Context, string, string, string) (providers.ComposePortDiscovery, error) {
	return e.discovery, nil
}
func TestAmbiguousComposeNeedsExplicitServiceAndPreservesAllWorkloads(t *testing.T) {
	engine := &testEngine{services: []dockerapi.ComposeApplicationService{{Name: "web", Role: "web"}, {Name: "admin", Role: "admin"}, {Name: "db", Role: "database"}}, discovery: providers.ComposePortDiscovery{Candidates: []providers.ComposePortCandidate{{Service: "web", ContainerPort: 8080, Protocol: "http"}, {Service: "admin", ContainerPort: 8080, Protocol: "http"}}, Infrastructure: []providers.ComposePortCandidate{{Service: "db", ContainerPort: 3306}}}}
	driver := New(engine, nil, nil, "")
	request := applications.PlanRequest{Application: applications.Application{ID: "app", Slug: "app"}, WorkDir: t.TempDir()}
	if _, err := driver.Plan(context.Background(), request); !errors.Is(err, applications.ErrConfigurationRequired) {
		t.Fatalf("ambiguous plan=%v", err)
	}
	request.Configuration = map[string]any{"compose_service": "web", "container_port": 8080, "host_port": 9080}
	plan, err := driver.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Workloads) != 3 {
		t.Fatal("database dropped from workload inventory")
	}
	count := 0
	for _, ep := range plan.Endpoints {
		if ep.Primary {
			count++
			if ep.Workload != "web" || ep.Name != "primary" || ep.HostPort != 9080 {
				t.Fatalf("primary=%+v", ep)
			}
		}
	}
	if count != 1 {
		t.Fatal("ambiguous primary remains")
	}
}
func TestManifestAliasOverridesOnlyReferencedComposeService(t *testing.T) {
	dir := t.TempDir()
	manifest := "version: 1\ndeployment:\n  driver: compose\nworkloads:\n  frontend:\n    service: web\n    role: api\n    primary: true\nendpoints:\n  main:\n    workload: frontend\n    protocol: http\n    container_port: 8080\n    primary: true\n    public: true\n"
	if err := os.WriteFile(filepath.Join(dir, "devbox.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	engine := &testEngine{services: []dockerapi.ComposeApplicationService{{Name: "web", Role: "web"}, {Name: "db", Role: "database"}}}
	result, err := New(engine, nil, nil, "").Detect(context.Background(), applications.DetectRequest{WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Endpoints) != 1 || result.Endpoints[0].Service != "web" || result.RequiresConfiguration {
		t.Fatalf("detection=%+v", result)
	}
	for _, service := range result.Services {
		if service.Name == "db" && (service.Primary || service.SuggestedRole != "database") {
			t.Fatal("alias override incorrectly modified database")
		}
	}
}

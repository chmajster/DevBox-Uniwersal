package managed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
)

func TestDetectWordPressProfileWithoutConfig(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"wp-admin", "wp-content", "wp-includes"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "wp-login.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := New(nil, runtimes.NewDefaultRegistry(), nil, nil, "")
	result, err := driver.Detect(context.Background(), applications.DetectRequest{SourceType: applications.SourceLocal, WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.Profile != "wordpress" || result.Runtime != "php" || len(result.Endpoints) != 1 || result.Endpoints[0].ContainerPort != 80 {
		t.Fatalf("WordPress detection = %+v", result)
	}
	selectedPHP, err := driver.Detect(context.Background(), applications.DetectRequest{SourceType: applications.SourceLocal, WorkDir: root, Configuration: map[string]any{"runtime": "php", "runtime_version": "8.5"}})
	if err != nil || selectedPHP.Profile != "wordpress" || selectedPHP.Runtime != "php" || selectedPHP.Version != "8.5" || selectedPHP.Endpoints[0].ContainerPort != 80 {
		t.Fatalf("selected PHP version should preserve the WordPress profile: %+v, %v", selectedPHP, err)
	}
}

func TestComposerPHPConstraintResolvesToRuntimeImageTag(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{"require":{"php":"^8.2"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.php"), []byte("<?php echo 'ok';"), 0o644); err != nil {
		t.Fatal(err)
	}
	driver := New(nil, runtimes.NewDefaultRegistry(), nil, nil, "")
	detection, err := driver.Detect(context.Background(), applications.DetectRequest{SourceType: applications.SourceLocal, WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if detection.Runtime != "php" || detection.Version != "8.2" {
		t.Fatalf("Composer PHP constraint should resolve to a concrete image tag: %+v", detection)
	}
	plan, err := driver.Plan(context.Background(), applications.PlanRequest{
		Application: applications.Application{ID: "composer-php"}, WorkDir: root, Detection: detection,
	})
	if err != nil {
		t.Fatalf("plan from Composer PHP constraint: %v", err)
	}
	if plan.Runtime == nil || plan.Runtime.Version != "8.2" {
		t.Fatalf("runtime image version = %+v, want 8.2", plan.Runtime)
	}
	configured, err := driver.Detect(context.Background(), applications.DetectRequest{
		SourceType: applications.SourceLocal, WorkDir: root,
		Configuration: map[string]any{"runtime": "php", "runtime_version": "^8.2"},
	})
	if err != nil {
		t.Fatalf("configured Composer constraint: %v", err)
	}
	if configured.Version != "8.2" {
		t.Fatalf("configured PHP image version = %q, want 8.2", configured.Version)
	}
}

func TestGenericProfilePlanCanRegenerateDeploymentSpecification(t *testing.T) {
	for _, runtime := range []string{"php", "node", "python", "go", "static"} {
		t.Run(runtime, func(t *testing.T) {
			root := t.TempDir()
			driver := New(nil, runtimes.NewDefaultRegistry(), nil, nil, "")
			config := map[string]any{"runtime": runtime, "runtime_version": containerspec.DefaultVersion(runtime)}
			detection, err := driver.Detect(context.Background(), applications.DetectRequest{SourceType: applications.SourceLocal, WorkDir: root, Configuration: config})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := driver.Plan(context.Background(), applications.PlanRequest{Application: applications.Application{ID: "generic-plan"}, WorkDir: root, Detection: detection, Configuration: config})
			if err != nil {
				t.Fatal(err)
			}
			profile, _ := plan.Runtime.Metadata["profile"].(string)
			if profile != "generic_"+runtime {
				t.Fatalf("profile=%q", profile)
			}
			// Deploy regenerates from persisted runtime metadata after allocating a host port.
			spec, err := driver.specification(plan.ApplicationID, root, plan.SourceRevision, applications.Source{}, plan.Runtime.Name, plan.Runtime.Version, profile, config, 18080)
			if err != nil {
				t.Fatalf("regenerate deployment specification: %v", err)
			}
			if spec.Fingerprint != plan.Metadata["fingerprint"] || spec.Image != plan.Workloads[0].Image {
				t.Fatal("Plan and Deploy must resolve the same fingerprint and image")
			}
		})
	}
}

type hostingEngine struct {
	engine
	built    []containerspec.DeploymentSpec
	deployed []containerspec.DeploymentSpec
	info     providers.ContainerInfo
}

func (e *hostingEngine) Available(context.Context) error                          { return nil }
func (e *hostingEngine) ManagedImageExists(context.Context, string) (bool, error) { return true, nil }
func (e *hostingEngine) BuildManaged(_ context.Context, s containerspec.DeploymentSpec) error {
	e.built = append(e.built, s)
	return nil
}
func (e *hostingEngine) ReplaceManagedPorts(_ context.Context, s containerspec.DeploymentSpec, _ []providers.PublishedPort) error {
	e.deployed = append(e.deployed, s)
	return nil
}
func (e *hostingEngine) Inspect(context.Context, string) (providers.ContainerInfo, error) {
	return e.info, nil
}

type hostingPorts struct{}

func (hostingPorts) Reserve(context.Context, string, string, string, *int) (providers.PortLease, error) {
	return providers.PortLease{Port: 32781}, nil
}
func (hostingPorts) Release(context.Context, string, string, int) error { return nil }

type hostingNetworks struct{ names []string }

func (n *hostingNetworks) EnsureNetwork(_ context.Context, name string) error {
	n.names = append(n.names, name)
	return nil
}
func TestManagedVersionRebuildIsIsolatedAndCarriesCommand(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "index.php"), []byte("<?php"), 0644)
	engine := &hostingEngine{info: providers.ContainerInfo{State: "running"}}
	networks := &hostingNetworks{}
	driver := New(engine, runtimes.NewDefaultRegistry(), hostingPorts{}, networks, "")
	for i, version := range []string{"8.2", "8.4"} {
		app := applications.Application{ID: "hosting-app"}
		config := map[string]any{"runtime": "php", "runtime_version": version, "start_command": "php -S 0.0.0.0:8080 -t ."}
		detected, err := driver.Detect(context.Background(), applications.DetectRequest{WorkDir: root, Configuration: config})
		if err != nil {
			t.Fatal(err)
		}
		plan, err := driver.Plan(context.Background(), applications.PlanRequest{Application: app, WorkDir: root, Detection: detected, Configuration: config})
		if err != nil {
			t.Fatal(err)
		}
		request := applications.ExecutionRequest{Application: app, WorkDir: root, ForceBuild: i == 1, Endpoints: []applications.Endpoint{{ID: "endpoint", Name: "web"}}}
		if _, err := driver.Deploy(context.Background(), request, plan); err != nil {
			t.Fatal(err)
		}
	}
	if len(engine.built) != 1 || engine.built[0].Version != "8.4" {
		t.Fatal("rebuild must bypass cached image")
	}
	if engine.deployed[0].Image == engine.deployed[1].Image || engine.deployed[0].ContainerName != engine.deployed[1].ContainerName {
		t.Fatal("runtime replacement identity invalid")
	}
	if engine.deployed[1].Command[0] != "php" || engine.deployed[1].BindMounts[root] != "/app" {
		t.Fatal("command/live source missing")
	}
	if len(networks.names) != 2 || networks.names[0] != "devbox-hosting-app" {
		t.Fatal("application used a shared network", networks.names)
	}
	for _, spec := range engine.deployed {
		for _, key := range []string{"devbox.managed", "devbox.project_id", "devbox.project_name"} {
			if _, ok := spec.Labels[key]; !ok {
				t.Fatal("missing ownership", key)
			}
		}
	}
}
func TestManagedRunningContainerWithoutPublicationIsUnhealthy(t *testing.T) {
	engine := &hostingEngine{info: providers.ContainerInfo{State: "running", PortBindings: []providers.ContainerPortBinding{}}}
	driver := New(engine, nil, nil, nil, "")
	port := 32781
	result, err := driver.Inspect(context.Background(), applications.InspectRequest{Workloads: []applications.Workload{{ID: "web", Name: "web", DriverResourceID: "container"}}, Endpoints: []applications.Endpoint{{WorkloadID: "web", ContainerPort: 8080, HostPort: &port, Public: true}}})
	if err != nil || len(result) != 1 || result[0].HealthState != applications.HealthUnhealthy {
		t.Fatalf("missing publication reported healthy: %+v %v", result, err)
	}
}

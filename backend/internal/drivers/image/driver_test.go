package image

import (
	"context"
	"errors"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type testEngine struct {
	engine
	cached  bool
	pulled  bool
	stopped []string
	started []string
}

func (e *testEngine) Available(context.Context) error { return nil }
func (e *testEngine) PullImage(context.Context, string) error {
	e.pulled = true
	e.cached = true
	return nil
}
func (e *testEngine) InspectImage(context.Context, string) (map[string]any, error) {
	if !e.cached {
		return nil, errors.New("image not cached")
	}
	return map[string]any{"Config": map[string]any{"ExposedPorts": map[string]any{"80/tcp": map[string]any{}}}}, nil
}
func (e *testEngine) Create(context.Context, providers.ContainerSpec) (providers.ContainerInfo, error) {
	return providers.ContainerInfo{}, errors.New("injected create failure")
}
func (e *testEngine) Stop(_ context.Context, id string) error {
	e.stopped = append(e.stopped, id)
	return nil
}
func (e *testEngine) Start(_ context.Context, id string) error {
	e.started = append(e.started, id)
	return nil
}

type testPorts struct {
	port     int
	released []int
}

func (p *testPorts) Reserve(context.Context, string, string, string, *int) (providers.PortLease, error) {
	return providers.PortLease{Port: p.port}, nil
}
func (p *testPorts) Release(_ context.Context, _, _ string, port int) error {
	p.released = append(p.released, port)
	return nil
}

func TestFirstImageDeploymentPullsBeforeResolvingPort(t *testing.T) {
	engine := &testEngine{}
	driver := New(engine, &testPorts{}, nil, "")
	plan, err := driver.Plan(context.Background(), applications.PlanRequest{Application: applications.Application{ID: "application"}, Source: applications.Source{DockerImage: "nginx:alpine"}})
	if err != nil || !engine.pulled || len(plan.Endpoints) != 1 || plan.Endpoints[0].ContainerPort != 80 {
		t.Fatalf("plan=%+v pulled=%v err=%v", plan, engine.pulled, err)
	}
}
func TestCreateFailureRestoresPreviousContainerAndOnlyReleasesNewLease(t *testing.T) {
	for _, samePort := range []bool{true, false} {
		t.Run(map[bool]string{true: "same-port", false: "replacement-port"}[samePort], func(t *testing.T) {
			oldPort := 8080
			port := 8081
			if samePort {
				port = oldPort
			}
			engine := &testEngine{}
			ports := &testPorts{port: port}
			driver := New(engine, ports, nil, "")
			req := applications.ExecutionRequest{Application: applications.Application{ID: "application"}, Deployment: applications.Deployment{ID: "deployment"}, Workloads: []applications.Workload{{Name: "app", DriverResourceID: "previous", ObservedState: applications.ObservedRunning}}, Endpoints: []applications.Endpoint{{ID: "endpoint", Name: "main", HostPort: &oldPort}}}
			plan := applications.DeploymentPlan{Workloads: []applications.PlannedWorkload{{Name: "app", Image: "nginx:alpine"}}, Endpoints: []applications.PlannedEndpoint{{Name: "main", Workload: "app", Protocol: "http", ContainerPort: 80}}}
			if _, err := driver.Deploy(context.Background(), req, plan); err == nil {
				t.Fatal("expected injected failure")
			}
			if samePort {
				if len(ports.released) != 0 || len(engine.stopped) != 1 || len(engine.started) != 1 || engine.started[0] != "previous" {
					t.Fatal("lost old container or owned lease")
				}
			} else {
				if len(ports.released) != 1 || ports.released[0] != 8081 || len(engine.stopped) != 0 {
					t.Fatal("new lease leaked or previous container interrupted")
				}
			}
		})
	}
}
func TestImageExposeDoesNotTreatUDPAsHTTP(t *testing.T) {
	raw := map[string]any{"Config": map[string]any{"ExposedPorts": map[string]any{"80/udp": map[string]any{}}}}
	if firstExposedPort(raw) != 0 {
		t.Fatal("UDP selected for HTTP")
	}
}

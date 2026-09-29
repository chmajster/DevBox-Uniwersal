package projects

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
)

func TestDeploymentIntegrationManagedAllocatesPortDetectsRuntimeBuildsAndRoutes(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{
		Runtime:         "",
		ContainerPolicy: ContainerPolicyAuto,
	})

	runtimeProvider := &integrationRuntime{name: "static", healthy: true, state: "stopped"}
	registry := runtimes.NewRegistry()
	if err := registry.Register(runtimeProvider); err != nil {
		t.Fatal(err)
	}
	ports := &integrationPorts{port: 18123}
	routes := &integrationRoutes{}
	managed := &integrationManaged{}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), registry, &testJobLogger{}, DeploymentIntegrations{
		Ports:   ports,
		Routes:  routes,
		Managed: managed,
	})

	_, err := handler.Run(context.Background(), domain.Job{
		ID: "integration-managed",
		Payload: map[string]any{
			"project_id":    project.ID,
			"deployment_id": deploymentID,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !managed.built || !managed.replaced {
		t.Fatalf("managed container pipeline incomplete: %+v", managed)
	}
	if managed.spec.Runtime != "static" || managed.spec.HostPort != 18123 {
		t.Fatalf("unexpected managed spec: %+v", managed.spec)
	}
	if routes.calls != 1 || routes.port != 18123 || routes.hostname != project.Slug+".localhost" {
		t.Fatalf("unexpected route reconciliation: %+v", routes)
	}

	refreshed, err := repo.Get(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Runtime != "static" {
		t.Fatalf("runtime = %q, want static", refreshed.Runtime)
	}
	if refreshed.Status != "running" {
		t.Fatalf("status = %q, want running", refreshed.Status)
	}
	state, err := repo.RuntimeContainerState(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Fingerprint == "" || state.ImageTag == "" || state.ContainerName == "" {
		t.Fatalf("managed container state was not persisted: %+v", state)
	}
}

func TestDeploymentIntegrationManagedProxyFailureIsWarning(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{
		Runtime:         "static",
		ContainerPolicy: ContainerPolicyAuto,
	})
	ports := &integrationPorts{port: 18125}
	routes := &integrationRoutes{returnError: errors.New("nginx unavailable")}
	managed := &integrationManaged{}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), runtimes.NewRegistry(), &testJobLogger{}, DeploymentIntegrations{
		Ports:   ports,
		Routes:  routes,
		Managed: managed,
	})

	result, err := handler.Run(context.Background(), domain.Job{
		ID: "integration-managed-proxy-warning",
		Payload: map[string]any{
			"project_id":    project.ID,
			"deployment_id": deploymentID,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	warnings, ok := result["warnings"].([]string)
	if !ok || len(warnings) != 1 || !strings.Contains(warnings[0], "reverse proxy") {
		t.Fatalf("warnings = %#v", result["warnings"])
	}
	refreshed, err := repo.Get(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != "running" {
		t.Fatalf("project status = %q, want running", refreshed.Status)
	}
	deployments, err := repo.ListDeployments(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) != 1 || deployments[0].Status != DeploymentSuccess {
		t.Fatalf("deployment should succeed with proxy warning: %+v", deployments)
	}
}

func TestDeploymentIntegrationManagedImageInspectionFailureUsesDependencyStage(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{
		Runtime:         "static",
		ContainerPolicy: ContainerPolicyAuto,
	})

	ports := &integrationPorts{port: 18126}
	managed := &integrationManaged{imageExistsErr: errors.New("invalid image reference")}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), runtimes.NewRegistry(), &testJobLogger{}, DeploymentIntegrations{
		Ports:   ports,
		Managed: managed,
	})

	_, err := handler.Run(context.Background(), domain.Job{
		ID: "integration-managed-image-inspection-failure",
		Payload: map[string]any{
			"project_id":    project.ID,
			"deployment_id": deploymentID,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "inspect managed image") {
		t.Fatalf("expected managed image inspection failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "devbox/runtime-") {
		t.Fatalf("error must identify the generated image reference, got %v", err)
	}

	deployments, err := repo.ListDeployments(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) != 1 {
		t.Fatalf("unexpected deployment count: %d", len(deployments))
	}
	if deployments[0].Status != DeploymentFailed || deployments[0].Stage != DeploymentDependencies {
		t.Fatalf("image inspection failure persisted at wrong stage: %+v", deployments[0])
	}
	if ports.released != 18126 {
		t.Fatalf("reserved port = %d was not released", ports.released)
	}
}

func TestDeploymentIntegrationManagedFailureReleasesPortAndPersistsFailure(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{
		Runtime:         "static",
		ContainerPolicy: ContainerPolicyAuto,
	})

	ports := &integrationPorts{port: 18124}
	routes := &integrationRoutes{}
	managed := &integrationManaged{replaceErr: context.DeadlineExceeded}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), runtimes.NewRegistry(), &testJobLogger{}, DeploymentIntegrations{
		Ports:   ports,
		Routes:  routes,
		Managed: managed,
	})

	_, err := handler.Run(context.Background(), domain.Job{
		ID: "integration-managed-failure",
		Payload: map[string]any{
			"project_id":    project.ID,
			"deployment_id": deploymentID,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "replace managed container") {
		t.Fatalf("expected managed container replacement failure, got %v", err)
	}
	if ports.released != 18124 {
		t.Fatalf("released port = %d, want 18124", ports.released)
	}
	if routes.calls != 0 {
		t.Fatalf("route was activated despite failed replacement: %+v", routes)
	}

	deployments, err := repo.ListDeployments(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) != 1 || deployments[0].Status != DeploymentFailed {
		t.Fatalf("failure was not persisted: %+v", deployments)
	}
}

func TestDeploymentIntegrationDockerComposeHealthThenRoute(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{
		ContainerPolicy: ContainerPolicyAuto,
		Healthcheck:     "http://127.0.0.1:19090/health",
	})
	if err := os.WriteFile(filepath.Join(project.LocalPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compose := &integrationCompose{binding: providers.ComposePortBinding{
		Service: "app", RequestedHostPort: 8080, HostPort: 8080, ContainerPort: 80, Protocol: "tcp",
	}}
	ports := &integrationPorts{port: 19090}
	routes := &integrationRoutes{}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), runtimes.NewRegistry(), &testJobLogger{}, DeploymentIntegrations{
		Ports:   ports,
		Routes:  routes,
		Compose: compose,
	})

	_, err := handler.Run(context.Background(), domain.Job{
		ID: "integration-compose",
		Payload: map[string]any{
			"project_id":    project.ID,
			"deployment_id": deploymentID,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !compose.validated || !compose.pulled || !compose.built || !compose.started || !compose.healthyChecked {
		t.Fatalf("compose pipeline incomplete: %+v", compose)
	}
	if routes.calls != 1 || routes.port != 19090 {
		t.Fatalf("proxy did not receive compose target port: %+v", routes)
	}
}

func TestDeploymentIntegrationDockerComposeProxyFailureIsWarning(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{
		ContainerPolicy: ContainerPolicyAuto,
		Healthcheck:     "http://127.0.0.1:19091/health",
	})
	if err := os.WriteFile(filepath.Join(project.LocalPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n    ports:\n      - \"19091:80\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compose := &integrationCompose{binding: providers.ComposePortBinding{
		Service: "app", RequestedHostPort: 19091, HostPort: 19091, ContainerPort: 80, Protocol: "tcp",
	}}
	ports := &integrationPorts{port: 19091}
	routes := &integrationRoutes{returnError: errors.New("nginx validation failed")}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), runtimes.NewRegistry(), &testJobLogger{}, DeploymentIntegrations{
		Ports:   ports,
		Routes:  routes,
		Compose: compose,
	})

	result, err := handler.Run(context.Background(), domain.Job{
		ID: "integration-compose-proxy-warning",
		Payload: map[string]any{
			"project_id":    project.ID,
			"deployment_id": deploymentID,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	warnings, ok := result["warnings"].([]string)
	if !ok || len(warnings) != 1 || !strings.Contains(warnings[0], "reverse proxy") {
		t.Fatalf("warnings = %#v", result["warnings"])
	}
	if compose.stopped {
		t.Fatal("healthy Compose application was stopped because reverse proxy failed")
	}
	refreshed, err := repo.Get(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != "running" {
		t.Fatalf("project status = %q, want running", refreshed.Status)
	}
	deployments, err := repo.ListDeployments(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments) != 1 || deployments[0].Status != DeploymentSuccess {
		t.Fatalf("deployment should succeed with proxy warning: %+v", deployments)
	}
}

func TestDeploymentIntegrationDockerComposeAllocatesNextFreePublishedPort(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{
		ContainerPolicy: ContainerPolicyAuto,
	})
	if err := os.WriteFile(filepath.Join(project.LocalPath, "compose.yaml"), []byte("services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compose := &integrationCompose{binding: providers.ComposePortBinding{
		Service: "web", RequestedHostPort: 8080, HostPort: 8080, ContainerPort: 80, Protocol: "tcp",
	}}
	ports := &integrationPorts{port: 8081}
	routes := &integrationRoutes{}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), runtimes.NewRegistry(), &testJobLogger{}, DeploymentIntegrations{
		Ports:   ports,
		Routes:  routes,
		Compose: compose,
	})

	_, err := handler.Run(context.Background(), domain.Job{
		ID: "integration-compose-next-port",
		Payload: map[string]any{
			"project_id":    project.ID,
			"deployment_id": deploymentID,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ports.reserveFromCalls != 1 || ports.reserveFromStart != 8080 {
		t.Fatalf("unexpected sequential reservation: %+v", ports)
	}
	if compose.upBinding == nil || compose.upBinding.HostPort != 8081 || compose.upBinding.ContainerPort != 80 {
		t.Fatalf("Compose up did not receive remapped stable port: %+v", compose.upBinding)
	}
	if routes.calls != 1 || routes.port != 8081 {
		t.Fatalf("proxy did not receive assigned Compose target port: %+v", routes)
	}
	if ports.released != 0 {
		t.Fatalf("successful Compose deployment released persistent port %d", ports.released)
	}
}

func TestDeploymentIntegrationDockerComposeReusesExistingProjectPort(t *testing.T) {
	repo, project, deploymentID := integrationProject(t, Project{
		ContainerPolicy: ContainerPolicyAuto,
	})
	if err := os.WriteFile(filepath.Join(project.LocalPath, "compose.yaml"), []byte("services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"8080:80\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := repo.db.ExecContext(context.Background(),
		"INSERT INTO ports(id,project_id,port,purpose,state,created_at) VALUES(?,?,?,?, 'reserved', ?)",
		NewID(), project.ID, 8082, "application", now); err != nil {
		t.Fatal(err)
	}

	compose := &integrationCompose{binding: providers.ComposePortBinding{
		Service: "web", RequestedHostPort: 8080, HostPort: 8080, ContainerPort: 80, Protocol: "tcp",
	}}
	ports := &integrationPorts{port: 8999}
	routes := &integrationRoutes{}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), runtimes.NewRegistry(), &testJobLogger{}, DeploymentIntegrations{
		Ports:   ports,
		Routes:  routes,
		Compose: compose,
	})

	_, err := handler.Run(context.Background(), domain.Job{
		ID: "integration-compose-stable-port",
		Payload: map[string]any{
			"project_id":    project.ID,
			"deployment_id": deploymentID,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ports.reserveFromCalls != 0 {
		t.Fatalf("existing project port must be reused without a new reservation: %+v", ports)
	}
	if compose.upBinding == nil || compose.upBinding.HostPort != 8082 {
		t.Fatalf("Compose did not reuse persisted host port 8082: %+v", compose.upBinding)
	}
	if routes.port != 8082 {
		t.Fatalf("reverse proxy port = %d, want stable port 8082", routes.port)
	}
}

func integrationProject(t *testing.T, overrides Project) (*Repository, Project, string) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "devbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(context.Background(), db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	project := Project{
		ID:              NewID(),
		Name:            "Integration App",
		Slug:            "integration-app",
		Status:          "ready",
		SourceType:      SourceLocal,
		LocalPath:       workDir,
		Runtime:         overrides.Runtime,
		RuntimeVersion:  overrides.RuntimeVersion,
		ContainerPolicy: overrides.ContainerPolicy,
		Healthcheck:     overrides.Healthcheck,
		AutoStart:       overrides.AutoStart,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if project.ContainerPolicy == "" {
		project.ContainerPolicy = ContainerPolicyAuto
	}
	repo := NewRepository(db)
	if err := repo.Create(context.Background(), project, ""); err != nil {
		t.Fatal(err)
	}
	deploymentID := NewID()
	if err := repo.CreateDeployment(context.Background(), Deployment{
		ID:        deploymentID,
		ProjectID: project.ID,
		Status:    DeploymentQueued,
		Stage:     DeploymentQueued,
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return repo, project, deploymentID
}

type integrationRuntime struct {
	name        string
	healthy     bool
	state       string
	started     bool
	stopped     bool
	restarted   bool
	lastProject runtimes.ProjectContext
}

func (r *integrationRuntime) Name() string { return r.name }

func (r *integrationRuntime) Detect(context.Context, runtimes.ProjectContext) (runtimes.Detection, error) {
	return runtimes.Detection{Detected: true, Runtime: r.name, Metadata: map[string]any{"confidence": 100}}, nil
}

func (r *integrationRuntime) Validate(context.Context, runtimes.ProjectContext) (runtimes.ValidationResult, error) {
	return runtimes.ValidationResult{Valid: true}, nil
}

func (r *integrationRuntime) InstallDependencies(context.Context, runtimes.ProjectContext) error {
	return nil
}

func (r *integrationRuntime) Build(context.Context, runtimes.ProjectContext) error { return nil }

func (r *integrationRuntime) Start(_ context.Context, project runtimes.ProjectContext) error {
	r.started = true
	r.lastProject = project
	r.state = "running"
	return nil
}

func (r *integrationRuntime) Stop(context.Context, runtimes.ProjectContext) error {
	r.stopped = true
	r.state = "stopped"
	return nil
}

func (r *integrationRuntime) Restart(_ context.Context, project runtimes.ProjectContext) error {
	r.restarted = true
	r.lastProject = project
	r.state = "running"
	return nil
}

func (r *integrationRuntime) Status(context.Context, runtimes.ProjectContext) (runtimes.ProcessStatus, error) {
	return runtimes.ProcessStatus{State: r.state}, nil
}

func (r *integrationRuntime) Logs(context.Context, runtimes.ProjectContext, runtimes.LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (r *integrationRuntime) HealthCheck(context.Context, runtimes.ProjectContext) (runtimes.HealthResult, error) {
	message := "ok"
	if !r.healthy {
		message = "unhealthy fixture"
	}
	return runtimes.HealthResult{Healthy: r.healthy, Message: message, CheckedAt: time.Now().UTC()}, nil
}

type integrationPorts struct {
	port             int
	released         int
	reserveFromCalls int
	reserveFromStart int
}

func (p *integrationPorts) Reserve(_ context.Context, projectID, purpose string, preferred *int) (providers.PortLease, error) {
	port := p.port
	if preferred != nil {
		port = *preferred
	}
	return providers.PortLease{Port: port, ProjectID: projectID, Purpose: purpose}, nil
}

func (p *integrationPorts) ReserveFrom(_ context.Context, projectID, purpose string, start int) (providers.PortLease, error) {
	p.reserveFromCalls++
	p.reserveFromStart = start
	return providers.PortLease{Port: p.port, ProjectID: projectID, Purpose: purpose}, nil
}

func (p *integrationPorts) Release(_ context.Context, port int) error {
	p.released = port
	return nil
}

func (p *integrationPorts) IsAvailable(context.Context, int) (bool, error) { return true, nil }

type integrationRoutes struct {
	calls       int
	projectID   string
	hostname    string
	port        int
	returnError error
}

func (r *integrationRoutes) EnsureProjectRoute(_ context.Context, projectID, hostname string, targetPort int) error {
	r.calls++
	r.projectID = projectID
	r.hostname = hostname
	r.port = targetPort
	return r.returnError
}

type integrationManaged struct {
	built          bool
	replaced       bool
	imageExists    bool
	imageExistsErr error
	replaceErr     error
	spec           containerspec.DeploymentSpec
}

func (m *integrationManaged) Available(context.Context) error { return nil }

func (m *integrationManaged) ManagedImageExists(context.Context, string) (bool, error) {
	return m.imageExists, m.imageExistsErr
}

func (m *integrationManaged) BuildManaged(_ context.Context, spec containerspec.DeploymentSpec) error {
	m.built = true
	m.spec = spec
	return nil
}

func (m *integrationManaged) ReplaceManaged(_ context.Context, spec containerspec.DeploymentSpec) error {
	m.replaced = true
	m.spec = spec
	return m.replaceErr
}

type integrationCompose struct {
	validated      bool
	pulled         bool
	built          bool
	started        bool
	stopped        bool
	healthyChecked bool
	bindingChecked bool
	binding        providers.ComposePortBinding
	bindingErr     error
	upBinding      *providers.ComposePortBinding
}

func (c *integrationCompose) Available(context.Context) error { return nil }

func (c *integrationCompose) ComposeValidate(context.Context, string, string) error {
	c.validated = true
	return nil
}

func (c *integrationCompose) ComposePull(context.Context, string, string, string) error {
	c.pulled = true
	return nil
}

func (c *integrationCompose) ComposeBuild(context.Context, string, string, string) error {
	c.built = true
	return nil
}

func (c *integrationCompose) ComposePortBinding(context.Context, string, string) (providers.ComposePortBinding, error) {
	c.bindingChecked = true
	return c.binding, c.bindingErr
}

func (c *integrationCompose) ComposeUp(_ context.Context, _, _, _ string, binding *providers.ComposePortBinding) error {
	c.started = true
	if binding != nil {
		copy := *binding
		c.upBinding = &copy
	}
	return nil
}

func (c *integrationCompose) ComposeDown(context.Context, string, string) error {
	c.stopped = true
	return nil
}

func (c *integrationCompose) ComposeHealthy(context.Context, string, string) error {
	c.healthyChecked = true
	return nil
}

func (c *integrationCompose) ComposeTargetPort(context.Context, string, string) (int, error) {
	if c.upBinding != nil {
		return c.upBinding.HostPort, nil
	}
	return 0, c.bindingErr
}

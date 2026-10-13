package applications

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type leakingBuildDriver struct {
	fixtureDriver
	value string
}

func (d *leakingBuildDriver) Build(ctx context.Context, _ ExecutionRequest, _ DeploymentPlan) error {
	jobs.EmitOutput(ctx, "stdout", "build diagnostic: "+d.value)
	jobs.EmitOutput(ctx, "stderr", "build warning: "+d.value)
	return errors.New("build failed: " + d.value)
}

func TestHostingBuildRedactsSecretsFromOutputAndFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	value := "private-build-value-123"
	if err := f.service.PutSecret(ctx, app.ID, "ACCESS_TOKEN", value); err != nil {
		t.Fatal(err)
	}
	driver := &leakingBuildDriver{fixtureDriver: *f.driver, value: value}
	registry := NewDriverRegistry()
	if err := registry.Register(driver); err != nil {
		t.Fatal(err)
	}
	f.service.drivers = registry
	handler := NewDeploymentActionHandler(f.service, f.runner, JobBuild)
	if err := f.runner.Register(handler); err != nil {
		t.Fatal(err)
	}
	deployment, job, err := f.service.EnqueueDeploymentAction(ctx, app.ID, nil, JobBuild)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := f.store.ClaimNext(ctx, job.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = handler.Run(ctx, claimed)
	if err == nil || strings.Contains(err.Error(), value) || !strings.Contains(err.Error(), "***") {
		t.Fatalf("build error leaked secret: %v", err)
	}
	stored, err := f.repo.Deployment(ctx, deployment.ID)
	if err != nil {
		t.Fatal(err)
	}
	var logs string
	if err := f.db.QueryRowContext(ctx, "SELECT COALESCE(group_concat(fields_json), '') FROM job_logs WHERE job_id=?", job.ID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"deployment": stored, "logs": logs})
	if err != nil || strings.Contains(string(raw), value) || !strings.Contains(string(raw), "***") {
		t.Fatalf("stored build output leaked secret: %s %v", raw, err)
	}
}

func TestHostingPublicEnvironmentHasDedicatedPersistence(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app, err := f.service.Create(ctx, CreateInput{Name: NewID(), SourceType: SourceEmpty, Configuration: map[string]any{"environment": map[string]string{"APP_ENV": "development"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var config, stored string
	if err := f.db.QueryRowContext(ctx, "SELECT source_config_json FROM applications WHERE id=?", app.ID).Scan(&config); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(config, "environment") || strings.Contains(config, "development") {
		t.Fatal("environment duplicated in application config")
	}
	if err := f.db.QueryRowContext(ctx, "SELECT value FROM application_environment_variables WHERE application_id=? AND name='APP_ENV'", app.ID).Scan(&stored); err != nil || stored != "development" {
		t.Fatalf("environment %q %v", stored, err)
	}
	got, err := f.service.Get(ctx, app.ID)
	if err != nil || decodeConfiguration(got.SourceConfig)["environment"] == nil {
		t.Fatal("environment missing from API", err)
	}
}

func TestHostingRejectsUnsafeCommandsPathsAndRemovedDrivers(t *testing.T) {
	f := newFixture(t)
	for _, config := range []map[string]any{{"start_command": "npm start; touch /tmp/injected"}, {"start_command": "python $(id)"}, {"domain": "bad;host"}, {"tls_mode": "letsencrypt"}, {"deployment_driver": "image"}, {"deployment_driver": "dockerfile"}} {
		if err := validateConfiguration(config); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted %v: %v", config, err)
		}
	}
	hidden := filepath.Join(f.root, ".ssh")
	if err := os.Mkdir(hidden, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(f.root, "source")
	if err := os.Symlink(hidden, alias); err != nil {
		t.Fatal(err)
	}
	f.service.allowedRoots = []string{f.root}
	for _, path := range []string{alias, hidden, filepath.Join(f.root, "..", "escape")} {
		if _, err := f.service.validateLocalPath(path); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted %s: %v", path, err)
		}
	}
}

func TestHostingFailedFirstDeploymentStaysFailedAfterReconciliation(t *testing.T) {
	f := newFixture(t)
	app := f.create(t)
	f.driver.deployErr = errors.New("invalid start executable")
	_, job := f.deploy(t, app.ID)
	if _, err := f.run(t, job); err == nil {
		t.Fatal("deployment succeeded")
	}
	state, err := f.service.State(context.Background(), app.ID)
	if err != nil || state.Status != "failed" {
		t.Fatalf("state %+v %v", state, err)
	}
}

func TestHostingRebuildAndRecreateHaveDistinctExecutionFlags(t *testing.T) {
	f := newFixture(t)
	app := f.create(t)
	_, job := f.deploy(t, app.ID)
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{JobRebuild, JobRecreate} {
		if err := f.runner.Register(NewDeploymentActionHandler(f.service, f.runner, kind)); err != nil {
			t.Fatal(err)
		}
		_, job, err := f.service.EnqueueDeploymentAction(context.Background(), app.ID, nil, kind)
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := f.store.ClaimNext(context.Background(), job.CreatedAt)
		if err != nil {
			t.Fatal(err)
		}
		result, err := NewDeploymentActionHandler(f.service, f.runner, kind).Run(context.Background(), claimed)
		if err != nil {
			t.Fatal(err)
		}
		if f.driver.lastRequest.ForceBuild != (kind == JobRebuild) || f.driver.lastRequest.Recreate != (kind == JobRecreate) {
			t.Fatalf("wrong action flags for %s", kind)
		}
		if err := f.store.CompleteJob(context.Background(), claimed.ID, result, job.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}
}

type exitingLifecycleDriver struct{ fixtureDriver }

func (d *exitingLifecycleDriver) Start(context.Context, InspectRequest) error {
	d.state = ObservedExited
	return nil
}
func TestHostingStartJobFailsForImmediatelyExitedProcess(t *testing.T) {
	f := newFixture(t)
	app := f.create(t)
	_, job := f.deploy(t, app.ID)
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	driver := &exitingLifecycleDriver{fixtureDriver: *f.driver}
	registry := NewDriverRegistry()
	if err := registry.Register(driver); err != nil {
		t.Fatal(err)
	}
	f.service.drivers = registry
	f.service.logs = fixtureLogs("process exited with error")
	job, err := f.service.EnqueueLifecycle(context.Background(), app.ID, "start", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(t, job); err == nil || !strings.Contains(err.Error(), "failed readiness") || !strings.Contains(err.Error(), "process exited") {
		t.Fatalf("start error %v", err)
	}
	state, err := f.service.State(context.Background(), app.ID)
	if err != nil || state.Status != "failed" {
		t.Fatalf("state %+v %v", state, err)
	}
}

type fixtureProxy struct {
	fail    bool
	applied []providers.ProxyRoute
	removed []string
}

func (p *fixtureProxy) Apply(_ context.Context, r providers.ProxyRoute) error {
	if p.fail {
		return errors.New("nginx validation failed")
	}
	p.applied = append(p.applied, r)
	return nil
}
func (p *fixtureProxy) Remove(_ context.Context, name string) error {
	p.removed = append(p.removed, name)
	return nil
}
func (p *fixtureProxy) Render(providers.ProxyRoute) (string, error) { return "", nil }
func (p *fixtureProxy) Reload(context.Context) error                { return nil }
func (p *fixtureProxy) Validate(context.Context) error              { return nil }
func TestHostingDomainActivationOwnershipAndSafeRemoval(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	_, job := f.deploy(t, app.ID)
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	proxy := &fixtureProxy{}
	f.service.WithRouting(proxy)
	if _, err := f.db.ExecContext(ctx, "UPDATE endpoints SET domain='app.local',tls_mode='existing' WHERE application_id=?", app.ID); err != nil {
		t.Fatal(err)
	}
	proxy.fail = true
	if err := f.service.syncRoutes(ctx, app.ID); err == nil {
		t.Fatal("invalid proxy activated")
	}
	var count int
	f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM application_routes").Scan(&count)
	if count != 0 {
		t.Fatal("failed route left ownership reserved")
	}
	proxy.fail = false
	if err := f.service.syncRoutes(ctx, app.ID); err != nil {
		t.Fatal(err)
	}
	endpoints, err := f.repo.Endpoints(ctx, app.ID)
	if err != nil || !endpoints[0].RouteActive || !proxy.applied[0].TLS {
		t.Fatalf("route %#v %v", endpoints, err)
	}
	other := f.create(t)
	_, job = f.deploy(t, other.ID)
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	f.db.ExecContext(ctx, "UPDATE endpoints SET domain='app.local' WHERE application_id=?", other.ID)
	if err := f.service.syncRoutes(ctx, other.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("domain reused %v", err)
	}
	job, err = f.service.EnqueueLifecycle(ctx, app.ID, "remove", nil, map[string]any{"options": DeleteOptions{RemoveContainers: true, DeleteConfiguration: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	if len(proxy.removed) != 1 || proxy.removed[0] != "app.local" {
		t.Fatal("route not removed")
	}
}

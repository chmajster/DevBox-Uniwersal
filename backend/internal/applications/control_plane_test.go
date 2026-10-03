package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	core "github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

// Test-only driver exercises the real service, SQLite, migrations and runner
// without requiring a privileged Docker socket for the unit test suite.
type fixtureDriver struct {
	name            string
	state           string
	inspectErr      error
	deployErr       error
	requirePort     bool
	deploymentCount int
	lastRequest     ExecutionRequest
	lastPlan        DeploymentPlan
	removed         bool
}

func (d *fixtureDriver) Name() string {
	if d.name != "" {
		return d.name
	}
	return "image"
}
func (d *fixtureDriver) Detect(context.Context, DetectRequest) (DetectionResult, error) {
	return DetectionResult{Driver: d.Name(), Confidence: "high", Runtime: "static"}, nil
}
func (d *fixtureDriver) Plan(_ context.Context, r PlanRequest) (DeploymentPlan, error) {
	if d.requirePort && r.Configuration["container_port"] == nil {
		return DeploymentPlan{}, ErrConfigurationRequired
	}
	return DeploymentPlan{Version: 1, ApplicationID: r.Application.ID, Driver: d.Name(), Workloads: []PlannedWorkload{{Name: "web", Role: "web", Primary: true, Image: "test-image"}}, Endpoints: []PlannedEndpoint{{Name: "web", Workload: "web", Protocol: "http", ContainerPort: 80, Public: true, Primary: true}}}, nil
}
func (d *fixtureDriver) Deploy(_ context.Context, r ExecutionRequest, p DeploymentPlan) (DeploymentResult, error) {
	d.lastRequest, d.lastPlan = r, p
	d.deploymentCount++
	if d.deployErr != nil {
		return DeploymentResult{}, d.deployErr
	}
	d.state = ObservedRunning
	return DeploymentResult{Resources: map[string]ResourceState{"web": {ResourceID: "test-container", ObservedState: ObservedRunning, HealthState: HealthHealthy}}, EndpointPorts: map[string]int{"web": 49152}}, nil
}
func (d *fixtureDriver) Inspect(_ context.Context, r InspectRequest) ([]ObservedWorkload, error) {
	if d.inspectErr != nil {
		return nil, d.inspectErr
	}
	out := []ObservedWorkload{}
	for _, w := range r.Workloads {
		out = append(out, ObservedWorkload{Name: w.Name, ResourceID: w.DriverResourceID, ObservedState: d.state, HealthState: HealthHealthy})
	}
	return out, nil
}
func (d *fixtureDriver) Start(context.Context, InspectRequest) error {
	d.state = ObservedRunning
	return nil
}
func (d *fixtureDriver) Stop(context.Context, InspectRequest) error {
	d.state = ObservedStopped
	return nil
}
func (d *fixtureDriver) Restart(context.Context, InspectRequest) error {
	d.state = ObservedRunning
	return nil
}
func (d *fixtureDriver) Remove(context.Context, InspectRequest) error {
	d.removed = true
	d.state = ObservedMissing
	return nil
}

type fixtureLogs string

func (l fixtureLogs) Logs(context.Context, string, int, bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(l))), nil
}

type fixture struct {
	db      *sql.DB
	service *Service
	repo    *Repository
	store   *core.SQLiteJobs
	runner  *jobs.Runner
	driver  *fixtureDriver
	root    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(context.Background(), db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	store := core.NewSQLiteJobs(db)
	runner := jobs.NewRunner(store)
	driver := &fixtureDriver{state: ObservedUnknown}
	registry := NewDriverRegistry()
	if err := registry.Register(driver); err != nil {
		t.Fatal(err)
	}
	key, err := secrets.GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewAESGCMFromBase64(key)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repo, registry, runner, nil, nil, nil, root).WithSecretStore(secrets.NewSQLiteStore(db, cipher))
	for _, h := range []jobs.Handler{NewDeployJobHandler(service, runner), NewDetectJobHandler(service, runner), NewLifecycleJobHandler(service, runner, JobStart), NewLifecycleJobHandler(service, runner, JobStop), NewLifecycleJobHandler(service, runner, JobRestart), NewLifecycleJobHandler(service, runner, JobRemove)} {
		if err := runner.Register(h); err != nil {
			t.Fatal(err)
		}
	}
	return &fixture{db, service, repo, store, runner, driver, root}
}
func (f *fixture) create(t *testing.T) Detail {
	t.Helper()
	app, err := f.service.Create(context.Background(), CreateInput{Name: NewID(), SourceType: SourceDockerImage, Source: SourceInput{DockerImage: "test-image"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return app
}
func (f *fixture) run(t *testing.T, queued domain.Job) (map[string]any, error) {
	t.Helper()
	ctx := context.Background()
	claimed, err := f.store.ClaimNext(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != queued.ID {
		t.Fatalf("claimed %s, want %s", claimed.ID, queued.ID)
	}
	var h jobs.Handler
	if claimed.Type == JobDeploy {
		h = NewDeployJobHandler(f.service, f.runner)
	} else {
		h = NewLifecycleJobHandler(f.service, f.runner, claimed.Type)
	}
	result, runErr := h.Run(ctx, claimed)
	if runErr != nil {
		if err := f.store.FailJob(ctx, claimed.ID, runErr.Error(), time.Now()); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := f.store.CompleteJob(ctx, claimed.ID, result, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return result, runErr
}
func (f *fixture) deploy(t *testing.T, id string) (Deployment, domain.Job) {
	t.Helper()
	dep, job, err := f.service.EnqueueDeploy(context.Background(), id, nil)
	if err != nil {
		t.Fatal(err)
	}
	return dep, job
}

func TestConfigurationStrictValidation(t *testing.T) {
	for _, raw := range []string{`{"host_port":1.5}`, `{"container_port":65536}`, `{"container_port":-1}`, `{"protocol":"udp"}`, `{"health_path":"https://evil.example/"}`, `{"root_dir":"/etc"}`, `{"root_dir":"../outside"}`, `{"environment":{"DB_PASSWORD":"private"}}`, `{"environment":{"DSN":"mysql://user:pass@db/one"}}`, `{"environment":{"nest":{"token":"private"}}}`, `{"unknown":true}`, `{"command":[1]}`} {
		var input map[string]any
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatal(err)
		}
		if err := validateConfiguration(input); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("accepted %s: %v", raw, err)
		}
	}
	if err := validateConfiguration(map[string]any{"container_port": 8080, "host_port": 0, "environment": map[string]string{"APP_ENV": "test"}, "modules": []string{"gd", "zip"}}); err != nil {
		t.Fatal(err)
	}
	if err := validateConfiguration(map[string]any{"root_dir": "apps/portal"}); err != nil {
		t.Fatalf("valid relative root_dir rejected: %v", err)
	}
}
func TestConfigurationWaitRecoveryAndStableResourceIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	f.driver.requirePort = true
	dep, job := f.deploy(t, app.ID)
	out, err := f.run(t, job)
	if err != nil || out["status"] != "waiting_for_configuration" {
		t.Fatalf("waiting=%v, %v", out, err)
	}
	stored, err := f.repo.Deployment(ctx, dep.ID)
	if err != nil || stored.FinishedAt == nil || stored.Status != "waiting_for_configuration" {
		t.Fatalf("deployment=%+v %v", stored, err)
	}
	config := map[string]any{"container_port": 80}
	if _, err := f.service.Update(ctx, app.ID, UpdateInput{Configuration: &config}); err != nil {
		t.Fatal(err)
	}
	_, job = f.deploy(t, app.ID)
	if _, err = f.run(t, job); err != nil {
		t.Fatal(err)
	}
	first, err := f.service.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "running" || first.DesiredState != DesiredRunning {
		t.Fatalf("state=%+v", first)
	}
	_, job = f.deploy(t, app.ID)
	if _, err = f.run(t, job); err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Get(ctx, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Workloads[0].ID != second.Workloads[0].ID || first.Endpoints[0].ID != second.Endpoints[0].ID {
		t.Fatal("redeployment replaced stable topology identities")
	}
	if f.driver.lastRequest.Workloads[0].DriverResourceID != "test-container" || *f.driver.lastRequest.Endpoints[0].HostPort != 49152 {
		t.Fatal("driver lost previous container/lease")
	}
	name := "Renamed application"
	if _, err := f.service.Update(ctx, app.ID, UpdateInput{Name: &name}); err != nil {
		t.Fatal(err)
	}
	renamed, _ := f.service.Get(ctx, app.ID)
	if renamed.Slug != app.Slug {
		t.Fatal("rename moved source directory identity")
	}
}
func TestOperationLockCancellationAndRetry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	dep, job := f.deploy(t, app.ID)
	if _, _, err := f.service.EnqueueDeploy(ctx, app.ID, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate=%v", err)
	}
	name := "busy"
	if _, err := f.service.Update(ctx, app.ID, UpdateInput{Name: &name}); !errors.Is(err, ErrConflict) {
		t.Fatalf("mutation not locked=%v", err)
	}
	if err := f.service.PutSecret(ctx, app.ID, "PASSWORD", "x"); !errors.Is(err, ErrConflict) {
		t.Fatalf("secret mutation not locked=%v", err)
	}
	claimed, err := f.store.ClaimNext(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.runner.Cancel(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.CheckIdle(ctx, app.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("cancel released lock before rollback=%v", err)
	}
	if err := f.store.FinishCancellation(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := f.repo.Deployment(ctx, dep.ID)
	if err != nil || stored.Status != "cancelled" {
		t.Fatalf("cancelled deployment=%+v %v", stored, err)
	}
	retry, err := f.runner.Retry(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.ApplicationID == nil || *retry.ApplicationID != app.ID {
		t.Fatal("retry lost application ownership")
	}
	if _, err := f.run(t, retry); err != nil {
		t.Fatal(err)
	}
	stored, _ = f.repo.Deployment(ctx, dep.ID)
	if stored.JobID == nil || *stored.JobID != retry.ID || stored.Status != "success" {
		t.Fatalf("retry did not replace binding=%+v", stored)
	}
	_, queued := f.deploy(t, app.ID)
	if err := f.runner.Cancel(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.CheckIdle(ctx, app.ID); err != nil {
		t.Fatal("queued cancellation left lock", err)
	}
}
func TestConcurrentEnqueueHasOneOwner(t *testing.T) {
	f := newFixture(t)
	app := f.create(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := f.service.EnqueueDeploy(context.Background(), app.ID, nil)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if !errors.Is(err, ErrConflict) {
				t.Errorf("enqueue: %v", err)
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("owners=%d", success)
	}
}
func TestSecretEncryptionInjectionAndRedaction(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	secret := "unique-private-value-not-in-plan-123"
	if err := f.service.PutSecret(ctx, app.ID, "DB_PASSWORD", secret); err != nil {
		t.Fatal(err)
	}
	names, err := f.service.SecretNames(ctx, app.ID)
	if err != nil || len(names) != 1 || names[0] != "DB_PASSWORD" {
		t.Fatalf("names=%v %v", names, err)
	}
	dep, job := f.deploy(t, app.ID)
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	if f.driver.lastRequest.SensitiveEnvironment["DB_PASSWORD"] != secret {
		t.Fatal("secret not injected")
	}
	stored, _ := f.repo.Deployment(ctx, dep.ID)
	if strings.Contains(string(stored.PlanSnapshot), secret) {
		t.Fatal("secret persisted in plan")
	}
	// SQLite files (including WAL) must contain ciphertext, never the submitted value.
	files, err := filepath.Glob(filepath.Join(f.root, "control.db*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("plaintext in %s", path)
		}
	}
	f.service.logs = fixtureLogs("connection: " + secret + "\nDB_PASSWORD=" + secret + "\n")
	logs, err := f.service.Logs(ctx, app.ID, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(logs)
	if strings.Contains(string(raw), secret) {
		t.Fatal("logs leaked secret")
	}
	f.driver.deployErr = fmt.Errorf("failure contained %s", secret)
	_, job = f.deploy(t, app.ID)
	_, err = f.run(t, job)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("deployment error leaked secret")
	}
	if err := f.service.DeleteSecret(ctx, app.ID, "DB_PASSWORD"); err != nil {
		t.Fatal(err)
	}
}
func TestProviderFailureInvalidatesCachedGreenState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	_, job := f.deploy(t, app.ID)
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	f.driver.inspectErr = errors.New("Docker unavailable")
	if _, err := f.service.State(ctx, app.ID); err == nil {
		t.Fatal("expected provider failure")
	}
	list, err := f.service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Status == "running" || list[0].ObservedState != ObservedUnknown {
		t.Fatal("unavailable provider left green list status")
	}
}
func TestTopologyStagesWithoutDroppingPreviousRuntime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	_, job := f.deploy(t, app.ID)
	if _, err := f.run(t, job); err != nil {
		t.Fatal(err)
	}
	w, e := materializeTopology(app.ID, DeploymentPlan{Workloads: []PlannedWorkload{{Name: "replacement", Role: "web", Primary: true}}, Endpoints: []PlannedEndpoint{{Name: "replacement", Workload: "replacement", Protocol: "http", ContainerPort: 8080, HostPort: 55000}}})
	if e[0].HostPort != nil {
		t.Fatal("requested port masquerades as observed publishing")
	}
	if err := f.repo.StageTopology(ctx, app.ID, w, e); err != nil {
		t.Fatal(err)
	}
	all, err := f.repo.Workloads(ctx, app.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("old runtime dropped during plan=%v %v", all, err)
	}
}
func TestApplicationPortLeaseReuseCollisionAndOwnership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create(t)
	b := f.create(t)
	makeEndpoint := func(id string) Endpoint {
		w, e := materializeTopology(id, DeploymentPlan{Workloads: []PlannedWorkload{{Name: "web", Role: "web"}}, Endpoints: []PlannedEndpoint{{Name: "web", Workload: "web", Protocol: "http", ContainerPort: 80}}})
		if err := f.repo.ReplaceTopology(ctx, id, w, e); err != nil {
			t.Fatal(err)
		}
		out, err := f.repo.Endpoints(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return out[0]
	}
	ae, be := makeEndpoint(a.ID), makeEndpoint(b.ID)
	socket, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := socket.Addr().(*net.TCPAddr).Port
	socket.Close()
	allocator := NewApplicationPortAllocator(f.db, port, port)
	lease, err := allocator.Reserve(ctx, a.ID, ae.ID, "application-http", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Reserve(ctx, b.ID, be.ID, "application-http", &lease.Port); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign collision=%v", err)
	}
	if err := allocator.Release(ctx, b.ID, be.ID, lease.Port); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign release=%v", err)
	}
	restarted := NewApplicationPortAllocator(f.db, 1, 1)
	same, err := restarted.Reserve(ctx, a.ID, ae.ID, "application-http", nil)
	if err != nil || same.Port != lease.Port {
		t.Fatal("restart lost durable lease", err)
	}
	if err := allocator.Release(ctx, a.ID, ae.ID, lease.Port); err != nil {
		t.Fatal(err)
	}
	if _, err := allocator.Reserve(ctx, b.ID, be.ID, "application-http", nil); err != nil {
		t.Fatal(err)
	}
}
func TestDeleteUndeployedAndPreserveLocalSources(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	app := f.create(t)
	job, err := f.service.EnqueueLifecycle(ctx, app.ID, "remove", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.run(t, job)
	if err != nil || out["deleted"] != true {
		t.Fatalf("undeployed removal=%v %v", out, err)
	}
	if _, err := f.repo.Get(ctx, app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("still exists=%v", err)
	}
	path := filepath.Join(f.root, "source")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	local, err := f.service.Create(ctx, CreateInput{Name: NewID(), SourceType: SourceLocal, Source: SourceInput{LocalPath: path}, Driver: "image"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err = f.service.EnqueueLifecycle(ctx, local.ID, "remove", nil, map[string]any{"options": DeleteOptions{RemoveContainers: true, RemoveSource: true, DeleteConfiguration: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.run(t, job); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("local deletion not rejected", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("local source changed", err)
	}
	if f.driver.removed {
		t.Fatal("destructive validation ran after external removal")
	}
}
func TestStrictJSONAndReadOnlyHTTPRole(t *testing.T) {
	f := newFixture(t)
	module := NewModule(f.service, nil)
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, api.ModuleMiddleware{Authenticate: func(h http.Handler) http.Handler { return h }, RequireRole: func(role domain.Role, h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if role != domain.RoleViewer {
				http.Error(w, "forbidden", 403)
				return
			}
			h.ServeHTTP(w, r)
		})
	}})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/applications", strings.NewReader(`{}`)))
	if response.Code != 403 {
		t.Fatalf("viewer mutation=%d", response.Code)
	}
	for _, raw := range []string{`{} {}`, `{} garbage`, `{"unexpected":true}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(raw))
		var input UpdateInput
		if err := decodeApplicationJSON(w, r, &input, false); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
func TestDetectionPrecedenceAndUnsafeGitInput(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, name := range []string{"compose", "dockerfile", "managed"} {
		if err := f.service.drivers.Register(&fixtureDriver{name: name}); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(f.root, "detection")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct{ file, body, want string }{{"index.html", "ok", "managed"}, {"Dockerfile", "FROM nginx:alpine", "dockerfile"}, {"compose.yaml", "services:\n  web:\n    image: nginx:alpine\n", "compose"}, {"devbox.yaml", "version: 1\ndeployment:\n  driver: dockerfile\n", "dockerfile"}} {
		write(test.file, test.body)
		result, err := f.service.selector.Detect(ctx, DetectRequest{SourceType: SourceLocal, WorkDir: dir}, "")
		if err != nil || result.Driver != test.want {
			t.Fatalf("%s selected %s: %v", test.file, result.Driver, err)
		}
	}
	if _, err := f.service.Detect(ctx, CreateInput{SourceType: SourceGit, Source: SourceInput{RepositoryURL: "https://user:private@example.invalid/repo", LocalPath: dir}}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("Git password persisted in analysis payload", err)
	}
	if _, err := f.service.Detect(ctx, CreateInput{SourceType: SourceLocal, Source: SourceInput{LocalPath: "/etc"}}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("outside allowed source accepted", err)
	}
}
func TestManifestSecretsAndIndentation(t *testing.T) {
	manifest, err := ParseManifest("version: 1\ndeployment:\n  driver: compose\nsecrets:\n  - DB_PASSWORD\nenvironment:\n  APP_ENV: test\n")
	if err != nil || len(manifest.Secrets) != 1 || manifest.Environment["APP_ENV"] != "test" {
		t.Fatalf("manifest=%+v %v", manifest, err)
	}
	for _, raw := range []string{"deployment:\n\tdriver: image\n", "deployment:\n   driver: image\n", "deployment: &alias\n"} {
		if _, err := ParseManifest(raw); err == nil {
			t.Fatalf("invalid manifest accepted=%q", raw)
		}
	}
}
func TestObservedAggregationIsOrderIndependent(t *testing.T) {
	a := []Workload{{ObservedState: ObservedStarting}, {ObservedState: ObservedFailed}}
	b := []Workload{a[1], a[0]}
	if aggregateObserved(a) != ObservedFailed || aggregateObserved(b) != ObservedFailed {
		t.Fatal("state depends on workload ordering")
	}
	for _, test := range []struct {
		desired string
		items   []Workload
		want    string
	}{
		{DesiredRunning, []Workload{{Primary: true, ObservedState: ObservedRunning, HealthState: HealthHealthy}}, "running"},
		{DesiredRunning, []Workload{{Primary: true, ObservedState: ObservedRunning}, {ObservedState: ObservedFailed}}, "degraded"},
		{DesiredRunning, []Workload{{Primary: true, ObservedState: ObservedFailed}}, "failed"},
		{DesiredStopped, []Workload{{Primary: true, ObservedState: ObservedExited}}, "stopped"},
		{DesiredRunning, []Workload{{Primary: true, ObservedState: ObservedUnknown}}, "unknown"},
	} {
		if got := AggregateStatus(test.desired, test.items); got != test.want {
			t.Errorf("got %s want %s", got, test.want)
		}
	}
}

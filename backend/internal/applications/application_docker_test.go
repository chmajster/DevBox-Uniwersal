package applications_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	dockerapi "github.com/chmajster/DevBox-Uniwersal/backend/internal/docker"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	composedriver "github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/compose"
	dockerfiledriver "github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/dockerfile"
	imagedriver "github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/image"
	manageddriver "github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/managed"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

// Explicit opt-in: the normal test suite never pulls images or requires a
// privileged socket. CI enables this job on a disposable Docker runner.
func TestApplicationDockerLifecycle(t *testing.T) {
	if os.Getenv("DEVBOX_TEST_APPLICATION_DOCKER") != "1" {
		t.Skip("set DEVBOX_TEST_APPLICATION_DOCKER=1 for real Docker lifecycle tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal(err)
	}
	engine := dockerapi.NewCLIProvider()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if err := engine.Available(ctx); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"image", "managed", "dockerfile", "compose"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			if err := os.Mkdir(source, 0755); err != nil {
				t.Fatal(err)
			}
			marker := "devbox-control-plane-" + kind
			write := func(path, body string) {
				if err := os.WriteFile(filepath.Join(source, path), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("index.html", marker)
			input := applications.CreateInput{Name: "acp-" + kind + "-" + applications.NewID(), SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: source}, Configuration: map[string]any{"environment": map[string]string{"APP_ENV": "test"}}}
			switch kind {
			case "image":
				input.SourceType = applications.SourceDockerImage
				input.Source = applications.SourceInput{DockerImage: "nginx:alpine"}
				input.Configuration["container_port"] = 80
				marker = "nginx"
			case "managed":
				input.Configuration["runtime"] = "static"
				input.Configuration["container_port"] = 8090
			case "dockerfile":
				write("Dockerfile", "FROM nginx:alpine\nCOPY index.html /usr/share/nginx/html/index.html\nEXPOSE 80\n")
			case "compose":
				write("compose.yaml", `services:
  web:
    image: nginx:alpine
    volumes:
      - ./index.html:/usr/share/nginx/html/index.html:ro
    expose: ["80"]
    depends_on: [cache]
  cache:
    image: redis:7-alpine
    expose: ["6379"]
    volumes:
      - data:/data
volumes:
  data:
`)
			}
			db, err := database.Open(filepath.Join(root, "control.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := database.Migrate(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
				t.Fatal(err)
			}
			repo := applications.NewRepository(db)
			store := repository.NewSQLiteJobs(db)
			runner := jobs.NewRunner(store)
			ports := applications.NewApplicationPortAllocator(db, 46000, 46999)
			registry := applications.NewDriverRegistry()
			for _, driver := range []applications.DeploymentDriver{imagedriver.New(engine, ports, engine, ""), manageddriver.New(engine, runtimes.NewDefaultRegistry(), ports, engine, ""), dockerfiledriver.New(engine, ports, engine, ""), composedriver.New(engine, ports, engine, "")} {
				if err := registry.Register(driver); err != nil {
					t.Fatal(err)
				}
			}
			key, err := secrets.GenerateMasterKey()
			if err != nil {
				t.Fatal(err)
			}
			cipher, err := secrets.NewAESGCMFromBase64(key)
			if err != nil {
				t.Fatal(err)
			}
			service := applications.NewService(repo, registry, runner, nil, engine, nil, root).WithSecretStore(secrets.NewSQLiteStore(db, cipher))
			handlers := map[string]jobs.Handler{}
			for _, handler := range []jobs.Handler{applications.NewDeployJobHandler(service, runner), applications.NewLifecycleJobHandler(service, runner, applications.JobStart), applications.NewLifecycleJobHandler(service, runner, applications.JobStop), applications.NewLifecycleJobHandler(service, runner, applications.JobRestart), applications.NewLifecycleJobHandler(service, runner, applications.JobRemove)} {
				if err := runner.Register(handler); err != nil {
					t.Fatal(err)
				}
				handlers[handler.Type()] = handler
			}
			app, err := service.Create(ctx, input, nil)
			if err != nil {
				t.Fatal(err)
			}
			// Clean only resources with this test's unpredictable application/project
			// labels, even when a failed assertion leaves a partial deployment.
			defer func() {
				cleanupCtx, done := context.WithTimeout(context.Background(), 60*time.Second)
				defer done()
				out, _ := exec.CommandContext(cleanupCtx, "docker", "ps", "-aq", "--filter", "label=io.devbox.application.id="+app.ID).Output()
				for _, id := range strings.Fields(string(out)) {
					_ = engine.Stop(cleanupCtx, id)
					_ = engine.Remove(cleanupCtx, id)
				}
				for _, resource := range []string{"network", "volume"} {
					out, _ := exec.CommandContext(cleanupCtx, "docker", resource, "ls", "-q", "--filter", "label=com.docker.compose.project="+app.Slug).Output()
					for _, id := range strings.Fields(string(out)) {
						_ = exec.CommandContext(cleanupCtx, "docker", resource, "rm", id).Run()
					}
				}
			}()
			secretValue := "e2e-value-" + applications.NewID()
			if err := service.PutSecret(ctx, app.ID, "APPLICATION_SECRET", secretValue); err != nil {
				t.Fatal(err)
			}
			run := func(job domain.Job) {
				t.Helper()
				claimed, err := store.ClaimNext(ctx, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				if claimed.ID != job.ID {
					t.Fatal("unexpected claimed job")
				}
				result, err := handlers[claimed.Type].Run(ctx, claimed)
				if err != nil {
					_ = store.FailJob(context.Background(), claimed.ID, err.Error(), time.Now())
					t.Fatalf("%s: %v", claimed.Type, err)
				}
				if err := store.CompleteJob(ctx, claimed.ID, result, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			deploy := func() {
				t.Helper()
				_, job, err := service.EnqueueDeploy(ctx, app.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				run(job)
			}
			inspect := func() applications.Detail {
				t.Helper()
				if _, err := service.State(ctx, app.ID); err != nil {
					t.Fatal(err)
				}
				value, err := service.Get(ctx, app.ID)
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			deploy()
			first := inspect()
			if first.Driver != kind || first.Status != "running" {
				t.Fatalf("driver=%s status=%s", first.Driver, first.Status)
			}
			var endpoint applications.Endpoint
			for _, candidate := range first.Endpoints {
				if candidate.Primary {
					endpoint = candidate
				}
			}
			if endpoint.HostPort == nil {
				t.Fatal("missing published endpoint")
			}
			port := *endpoint.HostPort
			checkHTTP := func() {
				t.Helper()
				client := &http.Client{Timeout: 5 * time.Second}
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
				response.Body.Close()
				if err != nil || response.StatusCode != 200 || !strings.Contains(strings.ToLower(string(data)), marker) {
					t.Fatalf("HTTP %d or unexpected application body", response.StatusCode)
				}
			}
			checkHTTP()
			for _, workload := range first.Workloads {
				out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .Config.Env}}", workload.DriverResourceID).Output()
				if err != nil || !strings.Contains(string(out), "APPLICATION_SECRET="+secretValue) {
					t.Fatal("encrypted secret not injected into workload", workload.Name)
				}
			}
			if kind == "compose" {
				if len(first.Workloads) != 2 {
					t.Fatal("Compose infrastructure missing from inventory")
				}
				for _, ep := range first.Endpoints {
					if ep.Primary && ep.ContainerPort == 6379 {
						t.Fatal("Redis selected for HTTP proxy")
					}
				}
			}
			deploy()
			second := inspect()
			found := false
			for _, ep := range second.Endpoints {
				if ep.Primary {
					found = true
					if ep.ID != endpoint.ID || ep.HostPort == nil || *ep.HostPort != port {
						t.Fatal("redeployment changed owned endpoint/host port")
					}
				}
			}
			if !found {
				t.Fatal("redeploy lost primary endpoint")
			}
			checkHTTP()
			for _, action := range []string{"stop", "start", "restart"} {
				job, err := service.EnqueueLifecycle(ctx, app.ID, action, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				run(job)
				state := inspect()
				if action == "stop" && state.Status != "stopped" {
					t.Fatalf("stop status=%s", state.Status)
				}
				if action != "stop" && state.Status != "running" {
					t.Fatalf("%s status=%s", action, state.Status)
				}
			}
			remove, err := service.EnqueueLifecycle(ctx, app.ID, "remove", nil, map[string]any{"options": applications.DeleteOptions{RemoveContainers: true, DeleteConfiguration: true}})
			if err != nil {
				t.Fatal(err)
			}
			run(remove)
			if _, err := service.Get(ctx, app.ID); err == nil {
				t.Fatal("application configuration not removed")
			}
			if _, err := os.Stat(filepath.Join(source, "index.html")); err != nil {
				t.Fatal("removal deleted local source", err)
			}
			if kind == "compose" {
				if err := exec.CommandContext(ctx, "docker", "volume", "inspect", app.Slug+"_data").Run(); err != nil {
					t.Fatal("persistent Compose volume deleted", err)
				}
			}
			rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if rows.Next() {
				t.Fatal("foreign key integrity violation")
			}
		})
	}
}

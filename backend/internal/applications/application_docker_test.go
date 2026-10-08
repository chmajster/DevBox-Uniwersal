package applications_test

import (
	"context"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	dockerapi "github.com/chmajster/DevBox-Uniwersal/backend/internal/docker"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	composedriver "github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/compose"
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := engine.Available(ctx); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"managed", "compose", "compose-yml", "auto-php", "auto-php82", "auto-node", "auto-python", "auto-go", "auto-wordpress", "auto-compose"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			engine := dockerapi.NewCLIProvider().WithRuntimeRoot(filepath.Join(root, "projects"))
			source := filepath.Join(root, "source")
			if err := os.Mkdir(source, 0755); err != nil {
				t.Fatal(err)
			}
			if os.Getuid() == 0 {
				if err := os.Chown(source, 10001, 10001); err != nil {
					t.Fatal(err)
				}
			}
			marker := "devbox-control-plane-" + kind
			write := func(path, body string) {
				if err := os.WriteFile(filepath.Join(source, path), []byte(body), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("index.html", marker)
			input := applications.CreateInput{Name: "acp-" + kind + "-" + applications.NewID(), SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: source}, Configuration: map[string]any{"environment": map[string]string{"APP_ENV": "test"}}}
			expectedDriver := kind
			var composeSource []byte
			switch kind {
			case "managed":
				input.Configuration["runtime"] = "static"
				input.Configuration["container_port"] = 8090
			case "compose", "compose-yml":
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
				composeSource, _ = os.ReadFile(filepath.Join(source, "compose.yaml"))
			case "auto-node":
				write("package.json", `{"private":true,"scripts":{"start":"node server.js"},"dependencies":{"is-number":"7.0.0"}}`)
				write("server.js", `require('is-number')(123);require('http').createServer((req,res)=>{require('fs').writeFileSync('/app/created-by-container.txt','rw');res.end('`+marker+`')}).listen(8080,'0.0.0.0')`)
				input.Configuration["deployment_mode"] = "auto"
				input.Configuration["runtime"] = "node"
				input.Configuration["runtime_version"] = "22"
				expectedDriver = "managed"
			case "auto-python":
				write("main.py", `from http.server import BaseHTTPRequestHandler,HTTPServer
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  open('/app/created-by-container.txt','w').write('rw')
  self.send_response(200);self.end_headers();self.wfile.write(b'`+marker+`')
HTTPServer(('0.0.0.0',8080),Handler).serve_forever()
`)
				input.Configuration["deployment_mode"] = "auto"
				input.Configuration["runtime"] = "python"
				input.Configuration["runtime_version"] = "3.13"
				expectedDriver = "managed"
			case "auto-go":
				write("go.mod", "module test.invalid/app\ngo 1.23\n")
				write("main.go", `package main
import("net/http";"os")
func main(){http.HandleFunc("/",func(w http.ResponseWriter,r *http.Request){os.WriteFile("/app/created-by-container.txt",[]byte("rw"),0644);w.Write([]byte("`+marker+`"))});http.ListenAndServe(":8080",nil)}`)
				input.Configuration["deployment_mode"] = "auto"
				input.Configuration["runtime"] = "go"
				input.Configuration["runtime_version"] = "1.26"
				expectedDriver = "managed"
			case "auto-php", "auto-php82":
				marker = "devbox-control-plane-" + kind
				write("index.php", "<?php file_put_contents(__DIR__.'/created-by-container.txt','rw'); echo '"+marker+"';")
				input.Configuration["deployment_mode"] = "auto"
				input.Configuration["runtime"] = "php"
				input.Configuration["runtime_version"] = "8.3"
				expectedDriver = "managed"
				if kind == "auto-php82" {
					input.Configuration["runtime_version"] = "8.2"
				}
			case "auto-wordpress":
				write("wp-settings.php", "<?php // WordPress bootstrap fixture")
				marker = "devbox-control-plane-auto-wordpress"
				for _, name := range []string{"wp-admin", "wp-content", "wp-includes"} {
					if err := os.Mkdir(filepath.Join(source, name), 0755); err != nil {
						t.Fatal(err)
					}
					if os.Getuid() == 0 {
						if err := os.Chown(filepath.Join(source, name), 10001, 10001); err != nil {
							t.Fatal(err)
						}
					}
				}
				write("index.php", "<?php file_put_contents(__DIR__.'/created-by-container.txt','rw'); echo '"+marker+"';")
				write("wp-login.php", "<?php echo 'wordpress';")
				input.Configuration["deployment_mode"] = "auto"
				input.Configuration["runtime"] = "php"
				input.Configuration["environment"] = map[string]string{"WORDPRESS_DB_HOST": "database.example.invalid", "WORDPRESS_DB_NAME": "wordpress", "WORDPRESS_DB_USER": "wordpress"}
				input.Configuration["runtime_version"] = "8.3"
				expectedDriver = "managed"
			case "auto-compose":
				write("compose.yaml", "services:\n  web:\n    image: nginx:alpine\n    volumes:\n      - ./index.html:/usr/share/nginx/html/index.html:ro\n    expose: ['80']\n")
				composeSource, _ = os.ReadFile(filepath.Join(source, "compose.yaml"))
				input.Configuration["deployment_mode"] = "auto"
				input.Configuration["runtime"] = "static"
				input.Configuration["runtime_version"] = "1.27"
				expectedDriver = "managed"
			}
			if kind == "compose-yml" {
				if err := os.Rename(filepath.Join(source, "compose.yaml"), filepath.Join(source, "docker-compose.yml")); err != nil {
					t.Fatal(err)
				}
				expectedDriver = "compose"
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
			for _, driver := range []applications.DeploymentDriver{manageddriver.New(engine, runtimes.NewDefaultRegistry(), ports, engine, ""), composedriver.New(engine, ports, engine, "")} {
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
			service := applications.NewService(repo, registry, runner, nil, engine, nil, root).WithSecretStore(secrets.NewSQLiteStore(db, cipher)).WithRuntimeCleanup(engine)
			handlers := map[string]jobs.Handler{}
			for _, handler := range []jobs.Handler{applications.NewDeployJobHandler(service, runner), applications.NewDeploymentActionHandler(service, runner, applications.JobRebuild), applications.NewDeploymentActionHandler(service, runner, applications.JobRecreate), applications.NewLifecycleJobHandler(service, runner, applications.JobStart), applications.NewLifecycleJobHandler(service, runner, applications.JobStop), applications.NewLifecycleJobHandler(service, runner, applications.JobRestart), applications.NewLifecycleJobHandler(service, runner, applications.JobRemove)} {
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
				_ = engine.RemoveApplicationNetwork(cleanupCtx, applications.DockerProjectName(app.ID))
				out, _ = exec.CommandContext(cleanupCtx, "docker", "image", "ls", "-q", "--filter", "label=io.devbox.application.id="+app.ID).Output()
				for _, id := range strings.Fields(string(out)) {
					_ = exec.CommandContext(cleanupCtx, "docker", "image", "rm", id).Run()
				}
				for _, resource := range []string{"network", "volume"} {
					out, _ := exec.CommandContext(cleanupCtx, "docker", resource, "ls", "-q", "--filter", "label=com.docker.compose.project="+applications.DockerProjectName(app.ID)).Output()
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
			if kind == "auto-wordpress" {
				if err := service.PutSecret(ctx, app.ID, "WORDPRESS_DB_PASSWORD", secretValue); err != nil {
					t.Fatal(err)
				}
			}
			deploy()
			if kind == "auto-wordpress" {
				generated, err := os.ReadFile(filepath.Join(source, "wp-config.php"))
				if err != nil || !strings.Contains(string(generated), "getenv('WORDPRESS_DB_PASSWORD')") || strings.Contains(string(generated), secretValue) {
					t.Fatal("WordPress config generation leaked credentials or was absent", err)
				}
			}
			first := inspect()
			versionCommand, expectedVersion := []string{}, ""
			switch kind {
			case "auto-php", "auto-wordpress":
				versionCommand, expectedVersion = []string{"php", "-r", "echo PHP_VERSION;"}, "8.3"
			case "auto-php82":
				versionCommand, expectedVersion = []string{"php", "-r", "echo PHP_VERSION;"}, "8.2"
			case "auto-node":
				versionCommand, expectedVersion = []string{"node", "--version"}, "v22."
			case "auto-python":
				versionCommand, expectedVersion = []string{"python", "--version"}, "Python 3.13"
			case "auto-go":
				versionCommand, expectedVersion = []string{"go", "version"}, "go version go1.26"
			}
			if len(versionCommand) > 0 {
				out, err := exec.CommandContext(ctx, "docker", append([]string{"exec", first.Workloads[0].DriverResourceID}, versionCommand...)...).CombinedOutput()
				if err != nil || !strings.HasPrefix(string(out), expectedVersion) {
					t.Fatalf("wrong container language version: %q %v", out, err)
				}
			}
			if strings.HasPrefix(kind, "auto-") && kind != "auto-compose" && kind != "auto-wordpress" {
				if _, err := os.Stat(filepath.Join(source, "created-by-container.txt")); err != nil {
					t.Fatalf("container write was not visible on host: %v", err)
				}
			}
			restarted := applications.NewService(applications.NewRepository(db), registry, runner, nil, engine, nil, root).WithSecretStore(secrets.NewSQLiteStore(db, cipher)).WithRuntimeCleanup(engine)
			if state, err := restarted.State(ctx, app.ID); err != nil || state.Status != "running" {
				t.Fatalf("restart reconciliation: %+v %v", state, err)
			}

			if first.Driver != expectedDriver || first.Status != "running" {
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
			if kind == "auto-wordpress" {
				if _, err := os.Stat(filepath.Join(source, "created-by-container.txt")); err != nil {
					response, _ := http.Get(fmt.Sprintf("http://127.0.0.1:%d/index.php", port))
					body := []byte{}
					if response != nil {
						body, _ = io.ReadAll(response.Body)
						response.Body.Close()
					}
					out, _ := exec.CommandContext(ctx, "docker", "exec", first.Workloads[0].DriverResourceID, "sh", "-c", "ls -ld /var/www/html; printenv APACHE_RUN_USER APACHE_RUN_GROUP; ps -eo pid,uid,gid,args; cat /etc/apache2/conf-enabled/devbox-index.conf").CombinedOutput()
					t.Fatalf("WordPress HTTP did not write through mount: %v; index.php=%q; runtime=%s", err, body, out)
				}
			}
			if kind == "auto-php" {
				write("index.php", "<?php echo 'host-edit-visible';")
				checkHTTP = func() {
					t.Helper()
					client := &http.Client{Timeout: 5 * time.Second}
					req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
					response, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
					response.Body.Close()
					if err != nil || response.StatusCode != 200 || !strings.Contains(string(data), "host-edit-visible") {
						t.Fatalf("host PHP edit was not visible in running container: status=%d body=%q err=%v", response.StatusCode, string(data), err)
					}
				}
				checkHTTP()
				containerID := first.Workloads[0].DriverResourceID
				uid, gid := strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
				if os.Getuid() == 0 {
					uid, gid = "10001", "10001"
				}
				if err := exec.CommandContext(ctx, "docker", "exec", "--user", uid+":"+gid, containerID, "sh", "-c", "mkdir -p /app/uploads && printf created > /app/uploads/devbox-test.txt").Run(); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(source, "uploads", "devbox-test.txt")); err != nil {
					t.Fatalf("container write did not appear on host: %v", err)
				}
			}
			if len(composeSource) > 0 {
				composeFile := "compose.yaml"
				if kind == "compose-yml" {
					composeFile = "docker-compose.yml"
				}
				after, err := os.ReadFile(filepath.Join(source, composeFile))
				if err != nil || string(after) != string(composeSource) {
					t.Fatal("deployment modified source Compose file")
				}
			}
			if kind == "auto-wordpress" {
				containerID := first.Workloads[0].DriverResourceID
				uid, gid := strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
				if os.Getuid() == 0 {
					uid, gid = "10001", "10001"
				}
				if err := exec.CommandContext(ctx, "docker", "exec", "--user", uid+":"+gid, containerID, "sh", "-c", "mkdir -p /var/www/html/wp-content/uploads && printf created > /var/www/html/wp-content/uploads/devbox-test.txt").Run(); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(source, "wp-content", "uploads", "devbox-test.txt")); err != nil {
					t.Fatalf("WordPress container write did not appear on host: %v", err)
				}
			}
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
			for _, jobType := range []string{applications.JobRebuild, applications.JobRecreate} {
				if kind == "auto-go" && jobType == applications.JobRebuild {
					data, _ := os.ReadFile(filepath.Join(source, "main.go"))
					nextMarker := marker + "-rebuilt"
					write("main.go", strings.ReplaceAll(string(data), marker, nextMarker))
					marker = nextMarker
				}
				if kind == "auto-php" && jobType == applications.JobRebuild {
					configuration := input.Configuration
					configuration["runtime_version"] = "8.4"
					if _, err := service.Update(ctx, app.ID, applications.UpdateInput{Configuration: &configuration}); err != nil {
						t.Fatal(err)
					}
				}
				_, job, err := service.EnqueueDeploymentAction(ctx, app.ID, nil, jobType)
				if err != nil {
					t.Fatal(err)
				}
				run(job)
				current := inspect()
				if current.Status != "running" {
					t.Fatal("rebuild/recreate lost running state")
				}
				if kind == "auto-php" && current.Runtime.Version != "8.4" {
					t.Fatal("runtime version was not applied")
				}
				checkHTTP()
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
				if err := exec.CommandContext(ctx, "docker", "volume", "inspect", applications.DockerProjectName(app.ID)+"_data").Run(); err != nil {
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

func TestApplicationDockerPHPIsolationAndFailedReplacement(t *testing.T) {
	if os.Getenv("DEVBOX_TEST_APPLICATION_DOCKER") != "1" {
		t.Skip("set DEVBOX_TEST_APPLICATION_DOCKER=1 for real Docker tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	engine := dockerapi.NewCLIProvider()
	if err := engine.Available(ctx); err != nil {
		t.Fatal(err)
	}
	specs := []containerspec.DeploymentSpec{}
	for _, version := range []string{"8.2", "8.4"} {
		source := t.TempDir()
		os.Chmod(source, 0755)
		if os.Getuid() == 0 {
			if err := os.Chown(source, 10001, 10001); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(source, "index.php"), []byte("<?php file_put_contents(__DIR__.'/written.txt','rw'); echo PHP_VERSION;"), 0644); err != nil {
			t.Fatal(err)
		}
		id := applications.NewID()
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		spec, err := containerspec.GenerateManagedProfile(id, source, "php", version, nil, "", port, "")
		if err != nil {
			t.Fatal(err)
		}
		spec.Networks = []string{applications.DockerProjectName(id)}
		spec.Labels["io.devbox.application.id"] = id
		spec.Labels["devbox.managed"] = "true"
		spec.Labels["devbox.project_id"] = id
		spec.Labels["devbox.project_name"] = "PHP isolation"
		specs = append(specs, spec)
		defer func(spec containerspec.DeploymentSpec) {
			cleanup, done := context.WithTimeout(context.Background(), time.Minute)
			defer done()
			_ = engine.Stop(cleanup, spec.ContainerName)
			_ = engine.Remove(cleanup, spec.ContainerName)
			_ = engine.RemoveApplicationNetwork(cleanup, spec.Networks[0])
			out, _ := exec.CommandContext(cleanup, "docker", "image", "ls", "-q", "--filter", "label=io.devbox.application.id="+spec.ProjectID).Output()
			for _, image := range strings.Fields(string(out)) {
				_ = exec.CommandContext(cleanup, "docker", "image", "rm", image).Run()
			}
		}(spec)
		if err := engine.EnsureApplicationNetwork(ctx, spec.Networks[0], spec.Labels); err != nil {
			t.Fatal(err)
		}
		if err := engine.BuildManaged(ctx, spec); err != nil {
			t.Fatal(err)
		}
		if err := engine.ReplaceManagedPorts(ctx, spec, nil); err != nil {
			t.Fatal(err)
		}
	}
	check := func(spec containerspec.DeploymentSpec, version string) {
		t.Helper()
		client := &http.Client{Timeout: 5 * time.Second}
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d", spec.HostPort))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || !strings.HasPrefix(string(body), version) {
			t.Fatalf("runtime body %q want %s (%v)", body, version, err)
		}
		if _, err := os.Stat(filepath.Join(spec.ContextDir, "written.txt")); err != nil {
			t.Fatal("container write absent on host", err)
		}
	}
	check(specs[0], "8.2")
	check(specs[1], "8.4")
	if specs[0].Networks[0] == specs[1].Networks[0] {
		t.Fatal("shared application network")
	}
	// Invalid executable and immediately exiting process must fail, preserving
	// both the previous application's port/container and the second application.
	for _, command := range [][]string{{"devbox-no-such-executable"}, {"php", "-r", "fwrite(STDERR,'exited immediately'); exit(7);"}} {
		replacement := specs[0]
		replacement.Command = command
		if err := engine.ReplaceManagedPorts(ctx, replacement, nil); err == nil {
			t.Fatal("invalid/exiting command was considered ready")
		}
		check(specs[0], "8.2")
		check(specs[1], "8.4")
	}
}

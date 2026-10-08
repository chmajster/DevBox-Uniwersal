package applications_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/databases"
	dockerapi "github.com/chmajster/DevBox-Uniwersal/backend/internal/docker"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	composedriver "github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/compose"
	manageddriver "github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/managed"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/sourcegit"
)

type hostingDockerFixture struct {
	t              *testing.T
	ctx            context.Context
	root, network  string
	db             *sql.DB
	engine         *dockerapi.CLIProvider
	app            *applications.Service
	sql            *databases.Service
	store          *repository.SQLiteJobs
	secrets        secrets.SecretStore
	servers        map[string]databases.DatabaseServerManager
	applicationIDs []string
}

func newHostingDockerFixture(t *testing.T) *hostingDockerFixture {
	t.Helper()
	if os.Getenv("DEVBOX_TEST_APPLICATION_DOCKER") != "1" {
		t.Skip("set DEVBOX_TEST_APPLICATION_DOCKER=1 for real Docker hosting tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	t.Cleanup(cancel)
	root := t.TempDir()
	network := "devbox-test-" + applications.NewID()
	engine := dockerapi.NewCLIProvider().WithRuntimeRoot(filepath.Join(root, "artifacts")).WithSharedAppNetwork(network)
	if err := engine.Available(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	key, _ := secrets.GenerateMasterKey()
	cipher, _ := secrets.NewAESGCMFromBase64(key)
	secretStore := secrets.NewSQLiteStore(db, cipher)
	store := repository.NewSQLiteJobs(db)
	runner := jobs.NewRunner(store)
	portStart := 48000
	if strings.Contains(t.Name(), "Sources") {
		portStart = 49000
	}
	ports := applications.NewApplicationPortAllocator(db, portStart, portStart+999).WithDockerPorts(engine.PublishedApplicationPorts)
	registry := applications.NewDriverRegistry()
	for _, driver := range []applications.DeploymentDriver{manageddriver.New(engine, runtimes.NewDefaultRegistry(), ports, engine, network), composedriver.New(engine, ports, engine, network)} {
		if err := registry.Register(driver); err != nil {
			t.Fatal(err)
		}
	}
	app := applications.NewService(applications.NewRepository(db), registry, runner, sourcegit.New(secretStore), engine, nil, root, root, os.Getenv("DEVBOX_TEST_WSL_ROOT")).WithSecretStore(secretStore).WithRuntimeCleanup(engine)
	for _, handler := range []jobs.Handler{applications.NewDeployJobHandler(app, runner), applications.NewDeploymentActionHandler(app, runner, applications.JobRebuild), applications.NewLifecycleJobHandler(app, runner, applications.JobRemove), applications.NewLifecycleJobHandler(app, runner, applications.JobStart), applications.NewLifecycleJobHandler(app, runner, applications.JobStop), applications.NewLifecycleJobHandler(app, runner, applications.JobRestart)} {
		if err := runner.Register(handler); err != nil {
			t.Fatal(err)
		}
	}
	mysql := databases.NewManagedMySQLManager(engine, secretStore, databases.ManagedMySQLConfig{Container: network + "-mysql", Network: network, Volume: network + "-mysql-data", AdminPort: -1})
	maria := databases.NewManagedMySQLManager(engine, secretStore, databases.ManagedMySQLConfig{Image: "mariadb:11.4", Container: network + "-mariadb", Network: network, Volume: network + "-mariadb-data", AdminPort: -1})
	pg := databases.NewManagedPostgreSQLManager(engine, secretStore, databases.ManagedPostgreSQLConfig{Container: network + "-postgres", Network: network, Volume: network + "-pg-data"})
	mysqlScope, mysqlRef := mysql.AdminSecretRef()
	mariaScope, mariaRef := maria.AdminSecretRef()
	mysqlProvider := databases.NewMySQLProvider(databases.MySQLConfig{DockerContainer: mysql.ContainerName(), AdminUser: "root", AdminSecretScope: mysqlScope, AdminSecretRef: mysqlRef, ApplicationEndpointHost: mysql.ContainerName()}, secretStore)
	mariaProvider := databases.NewMySQLProvider(databases.MySQLConfig{DockerContainer: maria.ContainerName(), ClientBinary: "mariadb", AdminUser: "root", AdminSecretScope: mariaScope, AdminSecretRef: mariaRef, ApplicationEndpointHost: maria.ContainerName()}, secretStore)
	pgProvider := databases.NewPostgreSQLProvider(databases.PostgreSQLConfig{Container: pg.ContainerName()}, secretStore)
	servers := map[string]databases.DatabaseServerManager{"mysql": mysql, "mariadb": maria, "postgresql": pg}
	sqlService, err := databases.NewService(databases.NewRepository(db), mysqlProvider, secretStore, runner, nil, nil, filepath.Join(root, "backups"), databases.WithDatabaseEngine("mariadb", mariaProvider), databases.WithDatabaseEngine("postgresql", pgProvider), databases.WithComposeDatabaseProvider(engine), databases.WithDatabaseServers(servers))
	if err != nil {
		t.Fatal(err)
	}
	app.WithDatabaseResolver(sqlService)
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	f := &hostingDockerFixture{t: t, ctx: ctx, root: root, network: network, db: db, engine: engine, app: app, sql: sqlService, store: store, secrets: secretStore, servers: servers}
	t.Cleanup(func() {
		cancel()
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Minute)
		defer done()
		for _, applicationID := range f.applicationIDs {
			item := applications.Application{ID: applicationID}
			out, _ := exec.CommandContext(cleanup, "docker", "ps", "-aq", "--filter", "label=io.devbox.application.id="+item.ID).Output()
			for _, id := range strings.Fields(string(out)) {
				_ = exec.CommandContext(cleanup, "docker", "rm", "-f", id).Run()
			}
			for _, resource := range []string{"network", "volume"} {
				out, _ := exec.CommandContext(cleanup, "docker", resource, "ls", "-q", "--filter", "label=com.docker.compose.project="+applications.DockerProjectName(item.ID)).Output()
				for _, id := range strings.Fields(string(out)) {
					_ = exec.CommandContext(cleanup, "docker", resource, "rm", id).Run()
				}
			}
			_ = engine.RemoveApplicationNetwork(cleanup, applications.DockerProjectName(item.ID))
			out, _ = exec.CommandContext(cleanup, "docker", "image", "ls", "-q", "--filter", "label=io.devbox.application.id="+item.ID).Output()
			for _, id := range strings.Fields(string(out)) {
				_ = exec.CommandContext(cleanup, "docker", "image", "rm", id).Run()
			}
		}
		for _, server := range servers {
			_ = exec.CommandContext(cleanup, "docker", "rm", "-f", server.ContainerName()).Run()
			_ = exec.CommandContext(cleanup, "docker", "volume", "rm", server.Volume()).Run()
		}
		_ = exec.CommandContext(cleanup, "docker", "network", "rm", network).Run()
	})
	return f
}
func (f *hostingDockerFixture) wait(job domain.Job, succeeds bool) domain.Job {
	f.t.Helper()
	for {
		current, err := f.store.ByID(f.ctx, job.ID)
		if err != nil {
			f.t.Fatal(err)
		}
		if current.Status == "succeeded" || current.Status == "failed" || current.Status == "cancelled" {
			if succeeds != (current.Status == "succeeded") {
				errorMessage := ""
				if current.Error != nil {
					errorMessage = *current.Error
				}
				f.t.Fatalf("%s status=%s error=%s", current.Type, current.Status, errorMessage)
			}
			return current
		}
		select {
		case <-f.ctx.Done():
			f.t.Fatal(f.ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
func (f *hostingDockerFixture) source(name string, files map[string]string) string {
	f.t.Helper()
	root := filepath.Join(f.root, name)
	if err := os.MkdirAll(root, 0755); err != nil {
		f.t.Fatal(err)
	}
	if os.Getuid() == 0 {
		if err := os.Chown(root, 10001, 10001); err != nil {
			f.t.Fatal(err)
		}
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			f.t.Fatal(err)
		}
	}
	return root
}
func (f *hostingDockerFixture) create(input applications.CreateInput) applications.Detail {
	f.t.Helper()
	app, err := f.app.Create(f.ctx, input, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	f.applicationIDs = append(f.applicationIDs, app.ID)
	return app
}
func (f *hostingDockerFixture) deploy(id string) applications.Detail {
	f.t.Helper()
	_, job, err := f.app.EnqueueDeploy(f.ctx, id, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	f.wait(job, true)
	return f.detail(id)
}
func (f *hostingDockerFixture) detail(id string) applications.Detail {
	f.t.Helper()
	state, err := f.app.State(f.ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	app, err := f.app.Get(f.ctx, id)
	if err != nil {
		f.t.Fatal(err)
	}
	if state.Status != "running" {
		f.t.Fatalf("application state %+v", state)
	}
	return app
}
func (f *hostingDockerFixture) http(app applications.Detail) string {
	f.t.Helper()
	port := 0
	for _, ep := range app.Endpoints {
		if ep.Primary && ep.HostPort != nil {
			port = *ep.HostPort
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		f.t.Fatalf("HTTP status=%d body=%s err=%v", response.StatusCode, data, err)
	}
	return string(data)
}
func (f *hostingDockerFixture) rebuild(id string) applications.Detail {
	f.t.Helper()
	_, job, err := f.app.EnqueueDeploymentAction(f.ctx, id, nil, applications.JobRebuild)
	if err != nil {
		f.t.Fatal(err)
	}
	f.wait(job, true)
	return f.detail(id)
}
func (f *hostingDockerFixture) sqlQuery(engine, db, statement string) string {
	f.t.Helper()
	server := f.servers[engine]
	args := []string{"exec", "-i", server.ContainerName()}
	var password []byte
	if engine == "postgresql" {
		args = append(args, "psql", "-X", "-U", "postgres", "-d", db, "-A", "-t", "-q", "-v", "ON_ERROR_STOP=1")
	} else {
		manager := server.(*databases.ManagedMySQLManager)
		scope, ref := manager.AdminSecretRef()
		password, _ = f.secrets.Get(f.ctx, scope, ref)
		defer clear(password)
		client := "mysql"
		if engine == "mariadb" {
			client = "mariadb"
		}
		args = []string{"exec", "-i", "-e", "MYSQL_PWD", server.ContainerName(), client, "-uroot", "--batch", "--skip-column-names", db}
	}
	cmd := exec.CommandContext(f.ctx, "docker", args...)
	cmd.Stdin = strings.NewReader(statement)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+string(password))
	output, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("admin SQL failed: %v %s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestApplicationDockerSharedDatabases(t *testing.T) {
	f := newHostingDockerFixture(t)
	// The installed portable probe is copied into each real application container.
	if os.Getenv("DEVBOX_DATABASE_PROBE") == "" {
		probe, err := filepath.Abs(filepath.Join("..", "..", "..", ".build", "devbox-dbcheck"))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("DEVBOX_DATABASE_PROBE", probe)
	}
	php := f.create(applications.CreateInput{Name: "sql-php", SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: f.source("php", map[string]string{"index.php": "<?php file_put_contents(__DIR__.'/rw.txt','php'); echo PHP_VERSION;"})}, Configuration: map[string]any{"deployment_mode": "auto", "runtime": "php", "runtime_version": "8.3"}})
	pythonRoot := f.source("fastapi", map[string]string{"requirements.txt": "fastapi\nuvicorn\n", "main.py": "from fastapi import FastAPI\nimport sys\napp=FastAPI()\n@app.get('/')\ndef index():\n open('/app/rw.txt','w').write('python')\n return {'runtime':sys.version}\n"})
	python := f.create(applications.CreateInput{Name: "sql-fastapi", SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: pythonRoot}, Configuration: map[string]any{"deployment_mode": "auto", "runtime": "python", "runtime_version": "3.13"}})
	for _, engine := range []string{"mysql", "postgresql", "mariadb"} {
		t.Run(engine, func(t *testing.T) {
			previousT := f.t
			f.t = t
			defer func() { f.t = previousT }()
			ids := []string{php.ID, python.ID}
			bindings := []providers.DatabaseConnection{}
			passwords := [][]byte{}
			for i, id := range ids {
				job, err := f.sql.QueueApplicationDatabase(f.ctx, id, databases.JobApplicationDatabaseProvision, map[string]any{"engine": engine, "name": fmt.Sprintf("%s_app_%d", engine, i), "username": fmt.Sprintf("%s_user_%d", engine, i)}, nil)
				if err != nil {
					t.Fatal(err)
				}
				f.wait(job, true)
				app := f.deploy(id)
				body := f.http(app)
				if i == 0 && !strings.HasPrefix(body, "8.3") {
					t.Fatalf("wrong PHP version: %s", body)
				}
				if i == 1 && !strings.Contains(body, "3.13") {
					t.Fatalf("wrong FastAPI/Python version: %s", body)
				}
				connection, password, found, err := f.sql.ResolveBoundApplicationDatabase(f.ctx, id)
				if err != nil || !found {
					t.Fatal(err)
				}
				bindings = append(bindings, connection)
				passwords = append(passwords, password)
				job, err = f.sql.QueueApplicationDatabase(f.ctx, id, databases.JobApplicationDatabaseTest, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				result := f.wait(job, true)
				if result.Result["query"] != "SELECT 1" {
					t.Fatalf("missing SQL execution evidence: %+v", result.Result)
				}
				files, err := f.engine.RuntimeArtifacts(id)
				if err != nil {
					t.Fatal(err)
				}
				for _, data := range files {
					if strings.Contains(string(data), string(password)) {
						t.Fatal("export leaked database password")
					}
				}
			}
			defer func() {
				for _, password := range passwords {
					clear(password)
				}
			}()
			for i, id := range ids {
				app := f.detail(id)
				other := bindings[i]
				other.Database = bindings[1-i].Database
				if err := f.engine.TestApplicationContainerDatabase(f.ctx, app.Workloads[0].DriverResourceID, other, passwords[i]); err == nil {
					t.Fatal("application account accessed another application's database")
				}
			}
			// SQL authentication remains valid across replacement and database restart.
			f.sqlQuery(engine, bindings[0].Database, "CREATE TABLE persisted (value INTEGER); INSERT INTO persisted VALUES (42);")
			f.rebuild(php.ID)
			if result := f.sqlQuery(engine, bindings[0].Database, "SELECT value FROM persisted;"); result != "42" {
				t.Fatalf("database data lost: %s", result)
			}
			if output, err := exec.CommandContext(f.ctx, "docker", "inspect", "--format", "{{json .HostConfig.PortBindings}}", f.servers[engine].ContainerName()).Output(); err != nil || strings.Contains(string(output), "HostPort") {
				t.Fatalf("SQL required host publication: %s %v", output, err)
			}
			manager := f.servers[engine]
			if err := manager.Action(f.ctx, "restart"); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(60 * time.Second)
			for {
				connection, password, _, _ := f.sql.ResolveBoundApplicationDatabase(f.ctx, php.ID)
				err := f.engine.TestApplicationContainerDatabase(f.ctx, f.detail(php.ID).Workloads[0].DriverResourceID, connection, password)
				clear(password)
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(err)
				}
				time.Sleep(time.Second)
			}
		})
	}
	// Existing Compose includes HTTP, worker, Redis and a private SQL service.
	composeRoot := f.source("compose", map[string]string{"index.html": "compose-http", "compose.yaml": `services:
  web:
    image: nginx:alpine
    volumes: ["./index.html:/usr/share/nginx/html/index.html:ro"]
    expose: ["80"]
  worker:
    image: python:3.13-bookworm
    command: ["python", "-c", "import time;time.sleep(3600)"]
    working_dir: /app
  cache:
    image: redis:7-alpine
    expose: ["6379"]
  database:
    image: postgres:17
    environment: {POSTGRES_HOST_AUTH_METHOD: trust}
    expose: ["5432"]
    volumes: ["data:/var/lib/postgresql/data"]
volumes:
  data:
`})
	original, _ := os.ReadFile(filepath.Join(composeRoot, "compose.yaml"))
	compose := f.create(applications.CreateInput{Name: "sql-compose", SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: composeRoot}, Configuration: map[string]any{"deployment_mode": "compose"}})
	binding, _, _ := f.sql.GetApplicationDatabaseBinding(f.ctx, php.ID)
	if _, err := f.sql.BindApplicationDatabaseUser(f.ctx, compose.ID, binding.DatabaseID, binding.UserID, nil, nil); err != nil {
		t.Fatal(err)
	}
	compose = f.deploy(compose.ID)
	if f.http(compose) != "compose-http" {
		t.Fatal("Compose HTTP failed")
	}
	for _, workload := range compose.Workloads {
		if workload.Name != "web" && workload.Name != "worker" {
			continue
		}
		output, err := exec.CommandContext(f.ctx, "docker", "inspect", "--format", "{{json .NetworkSettings.Networks}}", workload.DriverResourceID).Output()
		if err != nil || !strings.Contains(string(output), f.network) {
			t.Fatalf("missing shared network: %s", output)
		}
		job, err := f.sql.QueueApplicationDatabase(f.ctx, compose.ID, databases.JobApplicationDatabaseTest, map[string]any{"workload": workload.Name}, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.wait(job, true)
	}
	after, _ := os.ReadFile(filepath.Join(composeRoot, "compose.yaml"))
	if string(original) != string(after) {
		t.Fatal("source Compose was modified")
	}
	oldPort := compose.Endpoints[0].HostPort
	badConfig := map[string]any{"deployment_mode": "compose", "compose_service": "web", "container_port": 80, "host_port": 50100, "start_command": "devbox-no-such-command"}
	if _, err := f.app.Update(f.ctx, compose.ID, applications.UpdateInput{Configuration: &badConfig}); err != nil {
		t.Fatal(err)
	}
	_, failedJob, err := f.app.EnqueueDeploy(f.ctx, compose.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.wait(failedJob, false)
	compose = f.detail(compose.ID)
	if f.http(compose) != "compose-http" || compose.Endpoints[0].HostPort == nil || oldPort == nil || *compose.Endpoints[0].HostPort != *oldPort {
		t.Fatal("failed port/command change lost previous Compose configuration")
	}
	// Removing only this application keeps SQL servers, volumes and sources.
	job, err := f.app.EnqueueLifecycle(f.ctx, php.ID, "remove", nil, map[string]any{"options": applications.DeleteOptions{RemoveContainers: true, DeleteConfiguration: true}})
	if err != nil {
		t.Fatal(err)
	}
	f.wait(job, true)
	for _, manager := range f.servers {
		if _, err := f.engine.Inspect(f.ctx, manager.ContainerName()); err != nil {
			t.Fatal("application removal deleted shared SQL", err)
		}
		if err := exec.CommandContext(f.ctx, "docker", "volume", "inspect", manager.Volume()).Run(); err != nil {
			t.Fatal("shared SQL volume deleted", err)
		}
	}
	if _, err := os.Stat(php.Source.LocalPath); err != nil {
		t.Fatal("source removed", err)
	}
	job, err = f.sql.QueueApplicationDatabase(f.ctx, python.ID, databases.JobApplicationDatabaseTest, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.wait(job, true)
	manager := f.servers["mariadb"]
	if err := manager.Action(f.ctx, "stop"); err != nil {
		t.Fatal(err)
	}
	job, err = f.sql.QueueApplicationDatabase(f.ctx, python.ID, databases.JobApplicationDatabaseTest, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.wait(job, false)
	if err := manager.Action(f.ctx, "start"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(60 * time.Second); ; {
		connection, password, _, _ := f.sql.ResolveBoundApplicationDatabase(f.ctx, python.ID)
		err := f.engine.TestApplicationContainerDatabase(f.ctx, f.detail(python.ID).Workloads[0].DriverResourceID, connection, password)
		clear(password)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
	}
	public, _ := json.Marshal(compose)
	if strings.Contains(string(public), "DB_PASSWORD=") {
		t.Fatal("public application leaked credentials")
	}
}

func TestApplicationDockerSourcesAndRecovery(t *testing.T) {
	f := newHostingDockerFixture(t)
	t.Run("static-private-source", func(t *testing.T) {
		previousT := f.t
		f.t = t
		defer func() { f.t = previousT }()
		root := f.source("private-static", map[string]string{"index.html": "private-static"})
		if err := os.Chmod(root, 0750); err != nil {
			t.Fatal(err)
		}
		app := f.create(applications.CreateInput{Name: "private-static", SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: root}, Configuration: map[string]any{"deployment_mode": "auto", "runtime": "static", "runtime_version": "1.28"}})
		app = f.deploy(app.ID)
		if f.http(app) != "private-static" {
			t.Fatal("source-owner static HTTP failed")
		}
		if err := exec.CommandContext(f.ctx, "docker", "exec", app.Workloads[0].DriverResourceID, "sh", "-c", "printf rw > /usr/share/nginx/html/container.txt").Run(); err != nil {
			t.Fatal(err)
		}
		if body, _ := os.ReadFile(filepath.Join(root, "container.txt")); string(body) != "rw" {
			t.Fatal("static source mount was not writable")
		}
	})
	t.Run("dockerfile", func(t *testing.T) {
		previousT := f.t
		f.t = t
		defer func() { f.t = previousT }()
		root := f.source("custom", map[string]string{"index.html": "custom-workdir", "Dockerfile": "FROM python:3.13-bookworm\nWORKDIR /srv/site\nCOPY . .\nUSER 10001\nEXPOSE 8123\nCMD [\"python\",\"-m\",\"http.server\",\"8123\",\"--bind\",\"0.0.0.0\"]\n"})
		app := f.create(applications.CreateInput{Name: "dockerfile-source", SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: root}, Configuration: map[string]any{"deployment_mode": "dockerfile"}})
		app = f.deploy(app.ID)
		if f.http(app) != "custom-workdir" {
			t.Fatal("custom Dockerfile HTTP")
		}
		id := app.Workloads[0].DriverResourceID
		output, err := exec.CommandContext(f.ctx, "docker", "inspect", "--format", "{{json .Mounts}}", id).Output()
		if err != nil || !strings.Contains(string(output), "/srv/site") {
			t.Fatalf("WORKDIR mount missing: %s", output)
		}
		if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("live-edit"), 0644); err != nil {
			t.Fatal(err)
		}
		if f.http(app) != "live-edit" {
			t.Fatal("host edit hidden")
		}
		if err := exec.CommandContext(f.ctx, "docker", "exec", id, "sh", "-c", "printf rw > /srv/site/container.txt").Run(); err != nil {
			t.Fatal(err)
		}
		if data, _ := os.ReadFile(filepath.Join(root, "container.txt")); string(data) != "rw" {
			t.Fatal("container write hidden")
		}
		files, err := f.engine.RuntimeArtifacts(app.ID)
		if err != nil || !strings.Contains(string(files["Dockerfile"]), "WORKDIR /srv/site") {
			t.Fatal("Dockerfile export", err)
		}
		// A failed build preserves the previous working image/container/port.
		os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM python:3.13-bookworm\nWORKDIR /srv/site\nEXPOSE 8123\nRUN false\n"), 0644)
		_, job, err := f.app.EnqueueDeploymentAction(f.ctx, app.ID, nil, applications.JobRebuild)
		if err != nil {
			t.Fatal(err)
		}
		f.wait(job, false)
		if f.http(f.detail(app.ID)) != "live-edit" {
			t.Fatal("failed build destroyed previous app")
		}
	})
	t.Run("oci", func(t *testing.T) {
		previousT := f.t
		f.t = t
		defer func() { f.t = previousT }()
		app := f.create(applications.CreateInput{Name: "oci-source", SourceType: applications.SourceImage, Source: applications.SourceInput{DockerImage: "nginx:alpine"}, Configuration: map[string]any{"deployment_mode": "image", "container_port": 80}})
		app = f.deploy(app.ID)
		if !strings.Contains(f.http(app), "Welcome to nginx") {
			t.Fatal("OCI HTTP")
		}
		out, err := exec.CommandContext(f.ctx, "docker", "inspect", "--format", "{{json .Mounts}}", app.Workloads[0].DriverResourceID).Output()
		if err != nil || strings.Contains(string(out), `"Type":"bind"`) {
			t.Fatal("OCI received fictitious source mount", string(out), err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		previousT := f.t
		f.t = t
		defer func() { f.t = previousT }()
		app := f.create(applications.CreateInput{Name: "empty-source", SourceType: applications.SourceEmpty, Configuration: map[string]any{"deployment_mode": "auto", "runtime": "static", "runtime_version": "1.28"}})
		app = f.deploy(app.ID)
		if !strings.Contains(f.http(app), "DevBox") {
			t.Fatal("empty starter HTTP")
		}
	})
	t.Run("git", func(t *testing.T) {
		previousT := f.t
		f.t = t
		defer func() { f.t = previousT }()
		repo := f.source("git-origin", map[string]string{"index.html": "git-source"})
		for _, args := range [][]string{{"init", "-b", "main"}, {"add", "index.html"}, {"-c", "user.name=DevBox test", "-c", "user.email=test@devbox.invalid", "commit", "-m", "fixture"}} {
			cmd := exec.CommandContext(f.ctx, "git", append([]string{"-c", "safe.directory=" + repo, "-C", repo}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git fixture: %s %v", out, err)
			}
		}
		app := f.create(applications.CreateInput{Name: "git-source", SourceType: applications.SourceGit, Source: applications.SourceInput{RepositoryURL: repo, Reference: "main"}, Configuration: map[string]any{"deployment_mode": "auto", "runtime": "static", "runtime_version": "1.28"}})
		app = f.deploy(app.ID)
		if f.http(app) != "git-source" {
			t.Fatal("Git HTTP")
		}
		os.WriteFile(filepath.Join(repo, "index.html"), []byte("git-update"), 0644)
		cmd := exec.CommandContext(f.ctx, "git", "-c", "safe.directory="+repo, "-C", repo, "-c", "user.name=DevBox test", "-c", "user.email=test@devbox.invalid", "commit", "-am", "update")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git update: %s %v", out, err)
		}
		if f.http(f.deploy(app.ID)) != "git-update" {
			t.Fatal("Git redeploy did not pull")
		}
	})
	t.Run("wsl-windows-mount", func(t *testing.T) {
		previousT := f.t
		f.t = t
		defer func() { f.t = previousT }()
		base := os.Getenv("DEVBOX_TEST_WSL_ROOT")
		if base == "" {
			t.Skip("set DEVBOX_TEST_WSL_ROOT to an allowed /mnt/c directory")
		}
		if !strings.HasPrefix(base, "/mnt/c/") {
			t.Fatal("WSL test must exercise the actual Windows drive mapping")
		}
		root, err := os.MkdirTemp(base, "app-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(root)
		if os.Getuid() == 0 {
			if err := os.Chown(root, 10001, 10001); err != nil {
				t.Fatal(err)
			}
		}
		os.WriteFile(filepath.Join(root, "index.php"), []byte("<?php file_put_contents(__DIR__.'/rw.txt','windows-rw');echo 'windows-mount';"), 0644)
		app := f.create(applications.CreateInput{Name: "wsl-mount", SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: root}, Configuration: map[string]any{"deployment_mode": "auto", "runtime": "php", "runtime_version": "8.3"}})
		app = f.deploy(app.ID)
		if f.http(app) != "windows-mount" {
			t.Fatal("Windows source was not served")
		}
		if data, _ := os.ReadFile(filepath.Join(root, "rw.txt")); string(data) != "windows-rw" {
			t.Fatal("Windows bind mount was not writable")
		}
		os.WriteFile(filepath.Join(root, "index.php"), []byte("<?php echo 'windows-edited';"), 0644)
		if f.http(app) != "windows-edited" {
			t.Fatal("Windows edit was hidden")
		}
	})
	t.Run("errors-and-retry", func(t *testing.T) {
		previousT := f.t
		f.t = t
		defer func() { f.t = previousT }()
		source := f.source("errors", map[string]string{"index.html": "recovered"})
		listener, err := net.Listen("tcp4", "0.0.0.0:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		busy := f.create(applications.CreateInput{Name: "busy-port", SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: source}, Configuration: map[string]any{"deployment_mode": "auto", "runtime": "static", "runtime_version": "1.28", "host_port": port}})
		_, job, err := f.app.EnqueueDeploy(f.ctx, busy.ID, nil)
		if err != nil {
			listener.Close()
			t.Fatal(err)
		}
		f.wait(job, false)
		listener.Close()
		if f.http(f.deploy(busy.ID)) != "recovered" {
			t.Fatal("port recovery failed")
		}
		for _, config := range []map[string]any{{"runtime": "php", "runtime_version": "99.999"}, {"runtime": "static", "runtime_version": "1.28", "start_command": "devbox-executable-does-not-exist"}} {
			config["deployment_mode"] = "auto"
			app := f.create(applications.CreateInput{Name: "error-" + applications.NewID(), SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: source}, Configuration: config})
			_, job, err := f.app.EnqueueDeploy(f.ctx, app.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			f.wait(job, false)
			state, err := f.app.State(f.ctx, app.ID)
			if err != nil || state.Status == "running" {
				t.Fatal("failed deployment shown as running", state, err)
			}
			fixed := map[string]any{"runtime": "static", "runtime_version": "1.28", "deployment_mode": "auto"}
			if _, err := f.app.Update(f.ctx, app.ID, applications.UpdateInput{Configuration: &fixed}); err != nil {
				t.Fatal(err)
			}
			if f.http(f.deploy(app.ID)) != "recovered" {
				t.Fatal("retry failed")
			}
		}
		missing := f.create(applications.CreateInput{Name: "missing-image", SourceType: applications.SourceImage, Source: applications.SourceInput{DockerImage: "nginx:devbox-no-such-tag-999999"}, Configuration: map[string]any{"deployment_mode": "image", "container_port": 80}})
		_, job, err = f.app.EnqueueDeploy(f.ctx, missing.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		f.wait(job, false)
		if _, err := f.app.Create(f.ctx, applications.CreateInput{Name: "forbidden", SourceType: applications.SourceLocal, Source: applications.SourceInput{LocalPath: "/etc"}}, nil); err == nil {
			t.Fatal("forbidden source accepted")
		}
	})
}

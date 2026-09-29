package plugins

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func TestNormalizeHostIPsReturnsUsableNonLoopbackAddresses(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("192.168.1.20"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("172.17.0.1"), Mask: net.CIDRMask(16, 32)},
		&net.IPNet{IP: net.ParseIP("2001:db8::10"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("192.168.1.20"), Mask: net.CIDRMask(24, 32)},
	}

	got := normalizeHostIPs(addrs)
	want := []string{"172.17.0.1", "192.168.1.20", "2001:db8::10"}
	if len(got) != len(want) {
		t.Fatalf("unexpected addresses: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("address %d = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
}

func TestParsePostgreSQLClusters(t *testing.T) {
	items := parsePostgreSQLClusters("16 main 5432 online postgres /var/lib/postgresql/16/main /var/log/postgresql/postgresql-16-main.log\n15 legacy 5544 down postgres /var/lib/postgresql/15/legacy /var/log/postgresql/postgresql-15-legacy.log\n", "psql (PostgreSQL) 16.4")
	if len(items) != 2 {
		t.Fatalf("expected 2 clusters, got %d: %#v", len(items), items)
	}
	if items[0].Engine != "postgresql" || items[0].Port != 5432 || !items[0].Running {
		t.Fatalf("unexpected first cluster: %#v", items[0])
	}
	if items[0].Host != "host.docker.internal" || items[0].Label != "PostgreSQL 16 / main" {
		t.Fatalf("unexpected first cluster endpoint: %#v", items[0])
	}
	if items[1].Port != 5544 {
		t.Fatalf("custom PostgreSQL port was not preserved: %#v", items[1])
	}
}

func TestParsePostgreSQLClustersRejectsInvalidPort(t *testing.T) {
	items := parsePostgreSQLClusters("16 main invalid online postgres /data /log\n16 other 70000 online postgres /data /log\n", "psql")
	if len(items) != 0 {
		t.Fatalf("expected invalid clusters to be ignored, got %#v", items)
	}
}

type recordingPluginJobRunner struct {
	requests []jobs.Request
}

func (r *recordingPluginJobRunner) Register(jobs.Handler) error { return nil }

func (r *recordingPluginJobRunner) Enqueue(_ context.Context, request jobs.Request) (domain.Job, error) {
	r.requests = append(r.requests, request)
	return domain.Job{ID: "job-1", Type: request.Type, Status: "queued", RequestedBy: request.RequestedBy, Payload: request.Payload}, nil
}

func (r *recordingPluginJobRunner) Cancel(context.Context, string) error { return nil }

func (r *recordingPluginJobRunner) Retry(context.Context, string) (domain.Job, error) {
	return domain.Job{}, errors.New("not implemented")
}

type fakeDockerDatabaseServer struct {
	installed bool
	running   bool
	endpoint  providers.DatabaseEndpoint
	network   string
	container string
	image     string
	volume    string
}

func (f *fakeDockerDatabaseServer) Action(_ context.Context, action string) error {
	switch action {
	case "install", "start", "restart":
		f.installed = true
		f.running = true
	case "stop":
		f.running = false
	case "uninstall":
		f.installed = false
		f.running = false
	}
	return nil
}

func (f *fakeDockerDatabaseServer) ContainerState(context.Context) (bool, bool, error) {
	return f.installed, f.running, nil
}

func (f *fakeDockerDatabaseServer) ApplicationEndpoint() providers.DatabaseEndpoint {
	return f.endpoint
}
func (f *fakeDockerDatabaseServer) Network() string       { return f.network }
func (f *fakeDockerDatabaseServer) ContainerName() string { return f.container }
func (f *fakeDockerDatabaseServer) Image() string         { return f.image }
func (f *fakeDockerDatabaseServer) Volume() string        { return f.volume }

func TestQueueDockerDatabaseInstallsUseJobEngine(t *testing.T) {
	runner := &recordingPluginJobRunner{}
	mysqlServer := &fakeDockerDatabaseServer{
		endpoint: providers.DatabaseEndpoint{Host: "devbox-mysql", Port: 3306},
		network:  "devbox-apps", container: "devbox-mysql", image: "mysql:8.4", volume: "devbox-mysql-data",
	}
	postgresServer := &fakeDockerDatabaseServer{
		endpoint: providers.DatabaseEndpoint{Host: "devbox-postgresql", Port: 5432},
		network:  "devbox-apps", container: "devbox-postgresql", image: "postgres:17", volume: "devbox-postgresql-data",
	}
	service := NewService("", "", WithJobRunner(runner), WithMySQLDatabaseServer(mysqlServer), WithPostgreSQLDatabaseServer(postgresServer))
	actor := "admin-user"

	mysqlJob, err := service.QueueMySQLInstall(context.Background(), &actor)
	if err != nil {
		t.Fatal(err)
	}
	postgresJob, err := service.QueuePostgreSQLInstall(context.Background(), &actor)
	if err != nil {
		t.Fatal(err)
	}
	if mysqlJob.Type != JobInstallMySQLContainer || postgresJob.Type != JobInstallPostgreSQLContainer {
		t.Fatalf("unexpected job types: %q %q", mysqlJob.Type, postgresJob.Type)
	}
	if len(runner.requests) != 2 {
		t.Fatalf("expected two jobs, got %#v", runner.requests)
	}
	for _, request := range runner.requests {
		if request.Payload["runtime"] != "docker" || request.Payload["purpose"] != "application_database" {
			t.Fatalf("unexpected job payload: %#v", request.Payload)
		}
	}
}

func TestDockerDatabasePluginStatusUsesContainerDNS(t *testing.T) {
	mysqlServer := &fakeDockerDatabaseServer{
		installed: true, running: true,
		endpoint: providers.DatabaseEndpoint{Host: "devbox-mysql", Port: 3306},
		network:  "devbox-apps", container: "devbox-mysql", image: "mysql:8.4", volume: "devbox-mysql-data",
	}
	service := NewService("", "", WithMySQLDatabaseServer(mysqlServer))
	status := service.MySQLStatus(context.Background())
	if !status.Installed || !status.Running || status.ContainerHost != "devbox-mysql" || status.Network != "devbox-apps" {
		t.Fatalf("unexpected Docker MySQL status: %#v", status)
	}
}

func writeFakeDatabaseExecutable(t *testing.T, name, output string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	content := "#!/bin/sh\nprintf '%s\\n' '" + output + "'\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return path
}

func TestHostMySQLDiscoveryRemainsAvailableForExternalBindings(t *testing.T) {
	server := writeFakeDatabaseExecutable(t, "mysqld", "mysqld Ver 8.4.0 MySQL Community Server")
	instance, ok := detectHostMySQL(context.Background())
	if !ok || instance.Engine != "mysql" || instance.Source != server {
		t.Fatalf("unexpected host MySQL discovery: ok=%v instance=%#v", ok, instance)
	}
}

func TestQueueDockerDatabaseLifecycleActionsUseJobEngine(t *testing.T) {
	runner := &recordingPluginJobRunner{}
	mysqlServer := &fakeDockerDatabaseServer{
		installed: true, running: true,
		endpoint: providers.DatabaseEndpoint{Host: "devbox-mysql", Port: 3306},
		network:  "devbox-apps", container: "devbox-mysql", image: "mysql:8.4", volume: "devbox-mysql-data",
	}
	postgresServer := &fakeDockerDatabaseServer{
		installed: true, running: true,
		endpoint: providers.DatabaseEndpoint{Host: "devbox-postgresql", Port: 5432},
		network:  "devbox-apps", container: "devbox-postgresql", image: "postgres:17", volume: "devbox-postgresql-data",
	}
	service := NewService("", "", WithJobRunner(runner), WithMySQLDatabaseServer(mysqlServer), WithPostgreSQLDatabaseServer(postgresServer))
	actor := "admin-user"

	mysqlJob, err := service.QueueMySQLAction(context.Background(), "stop", &actor)
	if err != nil {
		t.Fatal(err)
	}
	postgresJob, err := service.QueuePostgreSQLAction(context.Background(), "uninstall", &actor)
	if err != nil {
		t.Fatal(err)
	}
	if mysqlJob.Type != JobMySQLContainerAction || postgresJob.Type != JobPostgreSQLContainerAction {
		t.Fatalf("unexpected lifecycle job types: %q %q", mysqlJob.Type, postgresJob.Type)
	}
	if runner.requests[0].Payload["action"] != "stop" || runner.requests[1].Payload["action"] != "uninstall" {
		t.Fatalf("unexpected lifecycle payloads: %#v", runner.requests)
	}
}

func TestDockerDatabaseLifecycleHandlersApplyActions(t *testing.T) {
	mysqlServer := &fakeDockerDatabaseServer{
		installed: true, running: true,
		endpoint: providers.DatabaseEndpoint{Host: "devbox-mysql", Port: 3306},
		network:  "devbox-apps", container: "devbox-mysql", image: "mysql:8.4", volume: "devbox-mysql-data",
	}
	service := NewService("", "", WithMySQLDatabaseServer(mysqlServer))

	status, err := service.MySQLAction(context.Background(), "stop")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || status.Running {
		t.Fatalf("unexpected status after stop: %#v", status)
	}
	status, err = service.MySQLAction(context.Background(), "restart")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || !status.Running {
		t.Fatalf("unexpected status after restart: %#v", status)
	}
	status, err = service.MySQLAction(context.Background(), "uninstall")
	if err != nil {
		t.Fatal(err)
	}
	if status.Installed || status.Running {
		t.Fatalf("unexpected status after uninstall: %#v", status)
	}
}

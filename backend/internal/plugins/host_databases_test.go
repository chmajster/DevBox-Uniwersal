package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

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

func TestQueueMySQLInstallUsesJobEngine(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	runner := &recordingPluginJobRunner{}
	service := NewService(
		"/usr/local/lib/devbox/devbox-helper",
		"/usr/bin/sudo",
		WithJobRunner(runner),
		withMySQLDetector(func(context.Context) (HostDatabaseInstance, bool) {
			return HostDatabaseInstance{}, false
		}),
	)
	actor := "admin-user"

	job, err := service.QueueMySQLInstall(context.Background(), &actor, 3307)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "job-1" || len(runner.requests) != 1 {
		t.Fatalf("unexpected queued job: %#v requests=%#v", job, runner.requests)
	}
	request := runner.requests[0]
	if request.Type != JobInstallHostMySQL {
		t.Fatalf("job type = %q, want %q", request.Type, JobInstallHostMySQL)
	}
	if request.RequestedBy == nil || *request.RequestedBy != actor {
		t.Fatalf("requested_by = %#v", request.RequestedBy)
	}
	if got := request.Payload["port"]; got != 3307 {
		t.Fatalf("queued port = %#v, want 3307", got)
	}
}

func TestMySQLInstallRejectedWhenManagedPortIsReserved(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	runner := &recordingPluginJobRunner{}
	service := NewService(
		"/usr/local/lib/devbox/devbox-helper",
		"/usr/bin/sudo",
		WithJobRunner(runner),
		WithReservedHostPort(3306, "zarządzany MySQL DevBox (devbox-mysql)"),
		withMySQLDetector(func(context.Context) (HostDatabaseInstance, bool) {
			return HostDatabaseInstance{}, false
		}),
	)

	status := service.MySQLStatus(context.Background())
	if !status.Installable {
		t.Fatalf("a free alternate port should keep host MySQL installable: %#v", status)
	}
	if status.SuggestedPort == 3306 {
		t.Fatalf("reserved managed port must not be suggested: %#v", status)
	}
	if _, err := service.QueueMySQLInstall(context.Background(), nil, 3306); err == nil {
		t.Fatal("expected queueing to fail for the reserved managed port")
	}
	if len(runner.requests) != 0 {
		t.Fatalf("conflicting installation must not enqueue a job: %#v", runner.requests)
	}
	if _, err := service.QueueMySQLInstall(context.Background(), nil, 3307); err != nil {
		t.Fatalf("alternate application port should be queueable: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("expected one queued alternate-port job, got %#v", runner.requests)
	}
}

func TestQueuePostgreSQLInstallUsesRequestedPort(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	runner := &recordingPluginJobRunner{}
	service := NewService(
		"/usr/local/lib/devbox/devbox-helper",
		"/usr/bin/sudo",
		WithJobRunner(runner),
	)
	job, err := service.QueuePostgreSQLInstall(context.Background(), nil, 5544)
	if err != nil {
		t.Fatal(err)
	}
	if job.Type != JobInstallHostPostgreSQL || len(runner.requests) != 1 {
		t.Fatalf("unexpected PostgreSQL job: %#v requests=%#v", job, runner.requests)
	}
	if got := runner.requests[0].Payload["port"]; got != 5544 {
		t.Fatalf("queued PostgreSQL port = %#v, want 5544", got)
	}
}

func TestJobPayloadPortAcceptsPersistedJSONNumber(t *testing.T) {
	port, err := jobPayloadPort(map[string]any{"port": float64(5432)})
	if err != nil {
		t.Fatal(err)
	}
	if port != 5432 {
		t.Fatalf("port = %d, want 5432", port)
	}
}

func TestLegacyControlPlaneMySQLIsNotExposedAsApplicationDatabase(t *testing.T) {
	service := NewService(
		"/usr/local/lib/devbox/devbox-helper",
		"/usr/bin/sudo",
		WithHostMySQLControlPlane(true),
		withMySQLDetector(func(context.Context) (HostDatabaseInstance, bool) {
			return HostDatabaseInstance{
				Engine: "mysql", Host: "host.docker.internal", Port: 3306,
				Installed: true, Running: true, ApplicationReady: true,
			}, true
		}),
	)
	status := service.MySQLStatus(context.Background())
	if status.Purpose != "control-plane" || status.ApplicationReady || status.Installable {
		t.Fatalf("legacy control-plane MySQL must be isolated from applications: %#v", status)
	}
	for _, item := range service.HostDatabases(context.Background()) {
		if item.Engine == "mysql" || item.Engine == "mariadb" {
			t.Fatalf("legacy control-plane MySQL must not be discoverable as app DB: %#v", item)
		}
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

func TestMySQLPluginStatusDetectsHostMySQL(t *testing.T) {
	server := writeFakeDatabaseExecutable(t, "mysqld", "mysqld  Ver 8.4.0 for Linux on x86_64 (MySQL Community Server)")
	service := NewService("/usr/local/bin/devbox-helper", "sudo")
	status := service.MySQLStatus(context.Background())
	if !status.Installed || status.Engine != "mysql" {
		t.Fatalf("unexpected MySQL plugin status: %#v", status)
	}
	if status.ServerPath != server || status.Port != 3306 || status.ContainerHost != "host.docker.internal" {
		t.Fatalf("unexpected MySQL endpoint: %#v", status)
	}
	if !status.Installable {
		t.Fatal("configured privileged helper should make MySQL installable")
	}
}

func TestMySQLPluginStatusDetectsMariaDB(t *testing.T) {
	writeFakeDatabaseExecutable(t, "mysqld", "mysqld  Ver 11.8.3-MariaDB for debian-linux-gnu on x86_64")
	service := NewService("", "")
	status := service.MySQLStatus(context.Background())
	if !status.Installed || status.Engine != "mariadb" {
		t.Fatalf("expected MariaDB detection, got %#v", status)
	}
}

package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

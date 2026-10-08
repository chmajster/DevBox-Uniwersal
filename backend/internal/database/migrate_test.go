package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrate(t *testing.T) {
	tmp := t.TempDir()
	migrations := filepath.Join(tmp, "migrations")
	if err := os.Mkdir(migrations, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migrations, "001_test.sql"), []byte("CREATE TABLE test_table (id TEXT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db, migrations); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := Migrate(context.Background(), db, migrations); err != nil {
		t.Fatalf("Migrate() idempotency error = %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(1) FROM schema_migrations WHERE version='001_test.sql'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one migration record, got %d", count)
	}
}

func TestHostingMigrationPreservesOCISource(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	migrations := filepath.Join(root, "old")
	if err := os.Mkdir(migrations, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join("..", "..", "..", "migrations")
	files, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range files {
		if entry.Name() > "017_hosting_environment.sql" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(migrations, entry.Name()), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := Open(filepath.Join(root, "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db, migrations); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO applications(id,name,slug,source_type,driver,created_at,updated_at) VALUES('oci','OCI','oci','docker_image','image',datetime('now'),datetime('now')); INSERT INTO application_sources(application_id,docker_image,created_at,updated_at) VALUES('oci','nginx:alpine',datetime('now'),datetime('now'));`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, source); err != nil {
		t.Fatal(err)
	}
	var image, driver, mode string
	if err := db.QueryRow(`SELECT s.docker_image,a.driver,json_extract(a.source_config_json,'$.deployment_mode') FROM applications a JOIN application_sources s ON s.application_id=a.id WHERE a.id='oci'`).Scan(&image, &driver, &mode); err != nil {
		t.Fatal(err)
	}
	if image != "nginx:alpine" || driver != "managed" || mode != "image" {
		t.Fatalf("lost OCI configuration: %q %q %q", image, driver, mode)
	}
	if err := Migrate(ctx, db, source); err != nil {
		t.Fatal("migration is not idempotent", err)
	}
}

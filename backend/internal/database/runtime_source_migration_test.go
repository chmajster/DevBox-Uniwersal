package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeSourceControlMigrationFromEmptyDatabase(t *testing.T) {
	migrations := repositoryMigrations(t)
	db, err := Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(context.Background(), db, migrations); err != nil {
		t.Fatalf("Migrate(empty) error = %v", err)
	}
	for _, table := range []string{"runtime_installations", "runtime_defaults", "project_runtime_assignments", "source_control_integrations", "project_source_control"} {
		assertTableExists(t, db, table)
	}
}

func TestRuntimeSourceControlMigrationUpgradesExistingDatabase(t *testing.T) {
	allMigrations := repositoryMigrations(t)
	oldMigrations := filepath.Join(t.TempDir(), "old-migrations")
	if err := os.MkdirAll(oldMigrations, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(allMigrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") || entry.Name() >= "006_runtime_versions_source_control.sql" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(allMigrations, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(oldMigrations, entry.Name()), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	db, err := Open(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(context.Background(), db, oldMigrations); err != nil {
		t.Fatalf("Migrate(pre-006) error = %v", err)
	}
	if err := Migrate(context.Background(), db, allMigrations); err != nil {
		t.Fatalf("Migrate(upgrade) error = %v", err)
	}
	assertTableExists(t, db, "runtime_installations")
	assertTableExists(t, db, "source_control_integrations")

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version='006_runtime_versions_source_control.sql'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("006 migration count = %d, want 1", count)
	}
}

func repositoryMigrations(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func assertTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("table %s does not exist", table)
	}
}

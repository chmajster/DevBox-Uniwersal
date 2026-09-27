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

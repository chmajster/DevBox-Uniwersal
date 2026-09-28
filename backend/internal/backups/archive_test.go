package backups

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSnapshotDatabaseCreatesValidSQLiteCopy(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO sample(value) VALUES ('ok')"); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(db, ManagerOptions{BackupDir: root})
	snapshot := filepath.Join(root, "snapshot.db")
	if err := manager.snapshotDatabase(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if err := validateSQLite(snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "bad.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	body := []byte("bad")
	if err := tw.WriteHeader(&tar.Header{Name: "../escape", Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive, filepath.Join(root, "stage")); err == nil {
		t.Fatal("expected traversal archive to be rejected")
	}
}

func TestPathWithinRoot(t *testing.T) {
	root := t.TempDir()
	if !pathWithinRoot(filepath.Join(root, "nested", "file"), root) {
		t.Fatal("expected nested path to be allowed")
	}
	if pathWithinRoot(filepath.Join(root, "..", "outside"), root) {
		t.Fatal("expected escaping path to be rejected")
	}
}

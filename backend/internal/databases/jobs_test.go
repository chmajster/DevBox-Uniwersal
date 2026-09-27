package databases

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type fakeBackupStore struct {
	database Database
	backup   Backup
	status   string
	size     int64
	message  *string
}

func (f *fakeBackupStore) DatabaseByID(_ context.Context, _ string) (Database, error) {
	return f.database, nil
}

func (f *fakeBackupStore) BackupByID(_ context.Context, _ string) (Backup, error) {
	return f.backup, nil
}

func (f *fakeBackupStore) CompleteBackup(_ context.Context, _ string, status string, size int64, message *string) error {
	f.status = status
	f.size = size
	f.message = message
	return nil
}

type fakeBackupEngine struct {
	dumpData   string
	dumpErr    error
	restoreErr error
}

func (f *fakeBackupEngine) DumpDatabase(_ context.Context, _ string, out io.Writer) error {
	if f.dumpErr != nil {
		return f.dumpErr
	}
	_, err := io.WriteString(out, f.dumpData)
	return err
}

func (f *fakeBackupEngine) RestoreDatabase(_ context.Context, _ string, _ io.Reader) error {
	return f.restoreErr
}

func TestBackupJobCreatesReadyBackup(t *testing.T) {
	root := t.TempDir()
	store := &fakeBackupStore{
		database: Database{ID: "db1", Name: "app_db"},
		backup:   Backup{ID: "backup1", DatabaseID: "db1", FileName: "backup.sql", Status: "queued"},
	}
	engine := &fakeBackupEngine{dumpData: "CREATE TABLE example(id INT);"}
	handler := NewBackupJobHandler(store, engine, root)
	result, err := handler.Run(context.Background(), domain.Job{Payload: map[string]any{"database_id": "db1", "backup_id": "backup1"}})
	if err != nil {
		t.Fatal(err)
	}
	if store.status != "ready" || store.size == 0 {
		t.Fatalf("unexpected backup completion state: status=%s size=%d", store.status, store.size)
	}
	if result["backup_id"] != "backup1" {
		t.Fatalf("unexpected job result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "backup.sql")); err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
}

func TestRestoreJobPropagatesFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "backup.sql"), []byte("SELECT 1;"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeBackupStore{
		database: Database{ID: "db1", Name: "app_db"},
		backup:   Backup{ID: "backup1", DatabaseID: "db1", FileName: "backup.sql", Status: "ready"},
	}
	engine := &fakeBackupEngine{restoreErr: errors.New("restore failed")}
	handler := NewRestoreJobHandler(store, engine, root)
	if _, err := handler.Run(context.Background(), domain.Job{Payload: map[string]any{"database_id": "db1", "backup_id": "backup1"}}); err == nil {
		t.Fatal("expected restore failure")
	}
}

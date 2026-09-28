package databases

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

const (
	JobTypeDatabaseBackup     = "database.backup"
	JobTypeDatabaseRestore    = "database.restore"
	JobTypeManagedMySQLAction = "database.mysql.action"
)

type backupStore interface {
	DatabaseByID(context.Context, string) (Database, error)
	BackupByID(context.Context, string) (Backup, error)
	CompleteBackup(context.Context, string, string, int64, *string) error
}

type backupEngine interface {
	DumpDatabase(context.Context, string, io.Writer) error
	RestoreDatabase(context.Context, string, io.Reader) error
}

type BackupJobHandler struct {
	store     backupStore
	engine    backupEngine
	backupDir string
}

func NewBackupJobHandler(store backupStore, engine backupEngine, backupDir string) *BackupJobHandler {
	return &BackupJobHandler{store: store, engine: engine, backupDir: backupDir}
}

func (h *BackupJobHandler) Type() string {
	return JobTypeDatabaseBackup
}

func (h *BackupJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	databaseID, err := payloadString(job.Payload, "database_id")
	if err != nil {
		return nil, err
	}
	backupID, err := payloadString(job.Payload, "backup_id")
	if err != nil {
		return nil, err
	}
	database, err := h.store.DatabaseByID(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	backup, err := h.store.BackupByID(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if backup.DatabaseID != database.ID {
		return nil, errors.New("backup does not belong to database")
	}
	path, err := backupPath(h.backupDir, backup.FileName)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(h.backupDir, 0o700); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create backup file: %w", err)
	}
	dumpErr := h.engine.DumpDatabase(ctx, database.Name, file)
	closeErr := file.Close()
	if dumpErr != nil || closeErr != nil {
		_ = os.Remove(path)
		message := "backup command failed"
		_ = h.store.CompleteBackup(ctx, backup.ID, "failed", 0, &message)
		if dumpErr != nil {
			return nil, dumpErr
		}
		return nil, fmt.Errorf("close backup file: %w", closeErr)
	}
	info, err := os.Stat(path)
	if err != nil {
		message := "backup stat failed"
		_ = h.store.CompleteBackup(ctx, backup.ID, "failed", 0, &message)
		return nil, fmt.Errorf("stat backup: %w", err)
	}
	if err := h.store.CompleteBackup(ctx, backup.ID, "ready", info.Size(), nil); err != nil {
		return nil, err
	}
	return map[string]any{"backup_id": backup.ID, "size_bytes": info.Size()}, nil
}

type RestoreJobHandler struct {
	store     backupStore
	engine    backupEngine
	backupDir string
}

func NewRestoreJobHandler(store backupStore, engine backupEngine, backupDir string) *RestoreJobHandler {
	return &RestoreJobHandler{store: store, engine: engine, backupDir: backupDir}
}

func (h *RestoreJobHandler) Type() string {
	return JobTypeDatabaseRestore
}

func (h *RestoreJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	databaseID, err := payloadString(job.Payload, "database_id")
	if err != nil {
		return nil, err
	}
	backupID, err := payloadString(job.Payload, "backup_id")
	if err != nil {
		return nil, err
	}
	database, err := h.store.DatabaseByID(ctx, databaseID)
	if err != nil {
		return nil, err
	}
	backup, err := h.store.BackupByID(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if backup.DatabaseID != database.ID || backup.Status != "ready" {
		return nil, errors.New("backup is not restorable for this database")
	}
	path, err := backupPath(h.backupDir, backup.FileName)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open backup: %w", err)
	}
	defer file.Close()
	if err := h.engine.RestoreDatabase(ctx, database.Name, file); err != nil {
		return nil, err
	}
	return map[string]any{"backup_id": backup.ID, "database_id": database.ID}, nil
}

func payloadString(payload map[string]any, key string) (string, error) {
	value, ok := payload[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("job payload field %s is required", key)
	}
	return value, nil
}

func backupPath(root, fileName string) (string, error) {
	if fileName == "" || filepath.Base(fileName) != fileName {
		return "", errors.New("invalid backup file name")
	}
	root = filepath.Clean(root)
	path := filepath.Join(root, fileName)
	if filepath.Dir(path) != root {
		return "", errors.New("backup path escapes backup directory")
	}
	return path, nil
}

type ManagedMySQLJobHandler struct {
	manager *ManagedMySQLManager
}

func NewManagedMySQLJobHandler(manager *ManagedMySQLManager) *ManagedMySQLJobHandler {
	return &ManagedMySQLJobHandler{manager: manager}
}

func (h *ManagedMySQLJobHandler) Type() string {
	return JobTypeManagedMySQLAction
}

func (h *ManagedMySQLJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	if h.manager == nil {
		return nil, errors.New("managed MySQL lifecycle is not configured")
	}
	action, err := payloadString(job.Payload, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "install", "start", "stop", "restart":
	default:
		return nil, fmt.Errorf("unsupported managed MySQL action %q", action)
	}
	if err := h.manager.Action(ctx, action); err != nil {
		return nil, err
	}
	return map[string]any{"action": action, "status": "completed"}, nil
}

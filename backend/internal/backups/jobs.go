package backups

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

const (
	JobCreateBackup  = "system.backup.create"
	JobImportBackup  = "system.backup.import"
	JobRestoreBackup = "system.backup.restore"
)

type CreateJobHandler struct {
	repo    *Repository
	manager *Manager
}

func NewCreateJobHandler(repo *Repository, manager *Manager) *CreateJobHandler {
	return &CreateJobHandler{repo: repo, manager: manager}
}

func (h *CreateJobHandler) Type() string { return JobCreateBackup }

func (h *CreateJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	id, err := jobPayloadString(job.Payload, "backup_id")
	if err != nil {
		return nil, err
	}
	item, err := h.repo.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := h.repo.UpdateStatus(ctx, id, "running", 0, "", "", false); err != nil {
		return nil, err
	}
	size, checksum, err := h.manager.CreateArchive(ctx, item)
	if err != nil {
		_ = h.repo.UpdateStatus(context.Background(), id, "failed", 0, "", err.Error(), true)
		return nil, err
	}
	if err := h.repo.UpdateStatus(ctx, id, "ready", size, checksum, "", true); err != nil {
		return nil, err
	}
	return map[string]any{"backup_id": id, "size_bytes": size, "sha256": checksum}, nil
}

type ImportJobHandler struct {
	repo      *Repository
	manager   *Manager
	backupDir string
}

func NewImportJobHandler(repo *Repository, manager *Manager, backupDir string) *ImportJobHandler {
	return &ImportJobHandler{repo: repo, manager: manager, backupDir: backupDir}
}

func (h *ImportJobHandler) Type() string { return JobImportBackup }

func (h *ImportJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	id, err := jobPayloadString(job.Payload, "backup_id")
	if err != nil {
		return nil, err
	}
	incoming, err := jobPayloadString(job.Payload, "incoming_path")
	if err != nil {
		return nil, err
	}
	incomingRoot := filepath.Join(h.backupDir, "incoming")
	if !pathWithinRoot(incoming, incomingRoot) {
		return nil, errors.New("incoming backup path is outside backup directory")
	}
	item, err := h.repo.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	validationDir := filepath.Join(h.backupDir, "import-validation", id)
	_, checksum, err := h.manager.ValidateAndExtractArchive(incoming, validationDir)
	_ = os.RemoveAll(validationDir)
	if err != nil {
		_ = h.repo.UpdateStatus(context.Background(), id, "failed", 0, "", err.Error(), true)
		_ = os.Remove(incoming)
		return nil, err
	}
	finalPath, err := h.manager.BackupPath(item.FileName)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(incoming, finalPath); err != nil {
		return nil, fmt.Errorf("activate imported backup: %w", err)
	}
	if err := os.Chmod(finalPath, 0o600); err != nil {
		return nil, err
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return nil, err
	}
	if err := h.repo.UpdateStatus(ctx, id, "ready", info.Size(), checksum, "", true); err != nil {
		return nil, err
	}
	return map[string]any{"backup_id": id, "size_bytes": info.Size(), "sha256": checksum}, nil
}

type RestoreJobHandler struct {
	repo      *Repository
	manager   *Manager
	backupDir string
}

func NewRestoreJobHandler(repo *Repository, manager *Manager, backupDir string) *RestoreJobHandler {
	return &RestoreJobHandler{repo: repo, manager: manager, backupDir: backupDir}
}

func (h *RestoreJobHandler) Type() string { return JobRestoreBackup }

func (h *RestoreJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	id, err := jobPayloadString(job.Payload, "backup_id")
	if err != nil {
		return nil, err
	}
	item, err := h.repo.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.Status != "ready" {
		return nil, errors.New("backup is not ready for restore")
	}
	if err := h.repo.UpdateStatus(ctx, id, "restoring", item.SizeBytes, item.SHA256, "", false); err != nil {
		return nil, err
	}
	marker, err := StageRestore(h.backupDir, item, h.manager)
	if err != nil {
		_ = h.repo.UpdateStatus(context.Background(), id, "failed", item.SizeBytes, item.SHA256, err.Error(), true)
		return nil, err
	}
	if err := h.repo.UpdateStatus(ctx, id, "pending_restore", item.SizeBytes, item.SHA256, "", false); err != nil {
		return nil, err
	}
	return map[string]any{
		"backup_id":        id,
		"restart_required": true,
		"staged_at":        marker.StagedAt,
	}, nil
}

func jobPayloadString(payload map[string]any, key string) (string, error) {
	value, ok := payload[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("job payload field %s is required", key)
	}
	return value, nil
}

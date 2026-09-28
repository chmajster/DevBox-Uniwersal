package backups

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

var ErrBusy = errors.New("backup operation is already in progress")

type Service struct {
	repo      *Repository
	jobs      jobs.JobRunner
	manager   *Manager
	backupDir string
}

func NewService(repo *Repository, runner jobs.JobRunner, manager *Manager, backupDir string) *Service {
	return &Service{repo: repo, jobs: runner, manager: manager, backupDir: backupDir}
}

func (s *Service) Handlers() []jobs.Handler {
	return []jobs.Handler{
		NewCreateJobHandler(s.repo, s.manager),
		NewImportJobHandler(s.repo, s.manager, s.backupDir),
		NewRestoreJobHandler(s.repo, s.manager, s.backupDir),
	}
}

func (s *Service) List(ctx context.Context) ([]Backup, error) {
	return s.repo.List(ctx)
}

func (s *Service) Create(ctx context.Context, actor *string) (Backup, error) {
	item := newBackup(actor, "queued")
	if err := s.repo.Create(ctx, item); err != nil {
		return Backup{}, err
	}
	if _, err := s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobCreateBackup,
		RequestedBy: actor,
		Payload:     map[string]any{"backup_id": item.ID},
	}); err != nil {
		_ = s.repo.UpdateStatus(ctx, item.ID, "failed", 0, "", err.Error(), true)
		return Backup{}, err
	}
	return s.repo.ByID(ctx, item.ID)
}

func (s *Service) Import(ctx context.Context, actor *string, source io.Reader) (Backup, error) {
	item := newBackup(actor, "importing")
	if err := os.MkdirAll(filepath.Join(s.backupDir, "incoming"), 0o700); err != nil {
		return Backup{}, err
	}
	incoming := filepath.Join(s.backupDir, "incoming", item.ID+".upload")
	file, err := os.OpenFile(incoming, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Backup{}, err
	}
	_, copyErr := io.Copy(file, source)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(incoming)
		return Backup{}, errors.Join(copyErr, closeErr)
	}
	if err := s.repo.Create(ctx, item); err != nil {
		_ = os.Remove(incoming)
		return Backup{}, err
	}
	if _, err := s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobImportBackup,
		RequestedBy: actor,
		Payload:     map[string]any{"backup_id": item.ID, "incoming_path": incoming},
	}); err != nil {
		_ = os.Remove(incoming)
		_ = s.repo.UpdateStatus(ctx, item.ID, "failed", 0, "", err.Error(), true)
		return Backup{}, err
	}
	return s.repo.ByID(ctx, item.ID)
}

func (s *Service) Restore(ctx context.Context, id string, actor *string) (Backup, error) {
	item, err := s.repo.ByID(ctx, id)
	if err != nil {
		return Backup{}, err
	}
	if item.Status != "ready" {
		return Backup{}, ErrBusy
	}
	if _, err := s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobRestoreBackup,
		RequestedBy: actor,
		Payload:     map[string]any{"backup_id": item.ID},
	}); err != nil {
		return Backup{}, err
	}
	return item, nil
}

func (s *Service) DownloadPath(ctx context.Context, id string) (Backup, string, error) {
	item, err := s.repo.ByID(ctx, id)
	if err != nil {
		return Backup{}, "", err
	}
	if item.Status != "ready" && item.Status != "pending_restore" {
		return Backup{}, "", ErrBusy
	}
	path, err := s.manager.BackupPath(item.FileName)
	if err != nil {
		return Backup{}, "", err
	}
	if _, err := os.Stat(path); err != nil {
		return Backup{}, "", err
	}
	return item, path, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	item, err := s.repo.ByID(ctx, id)
	if err != nil {
		return err
	}
	switch item.Status {
	case "running", "importing", "restoring", "pending_restore":
		return ErrBusy
	}
	path, err := s.manager.BackupPath(item.FileName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.repo.Delete(ctx, id)
}

func newBackup(actor *string, status string) Backup {
	id := randomID()
	return Backup{
		ID:          id,
		FileName:    fmt.Sprintf("devbox-%s-%s.tar.gz", time.Now().UTC().Format("20060102T150405Z"), id[:12]),
		Status:      status,
		RequestedBy: actor,
		CreatedAt:   time.Now().UTC(),
	}
}

func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

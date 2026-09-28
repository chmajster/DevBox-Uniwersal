package backups

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ApplyRestoreOptions struct {
	DatabasePath        string
	BackupDir           string
	ProjectsRoot        string
	NginxSitesAvailable string
	NginxSitesEnabled   string
}

type ApplyRestoreResult struct {
	Applied      bool
	BackupID     string
	RollbackPath string
}

func PendingRestorePath(backupDir string) string {
	return filepath.Join(backupDir, "restore-pending.json")
}

func StageRestore(backupDir string, item Backup, manager *Manager) (PendingRestore, error) {
	archivePath, err := manager.BackupPath(item.FileName)
	if err != nil {
		return PendingRestore{}, err
	}
	stagingRoot := filepath.Join(backupDir, "restore-staging")
	if err := os.MkdirAll(stagingRoot, 0o700); err != nil {
		return PendingRestore{}, fmt.Errorf("create restore staging root: %w", err)
	}
	stagingDir := filepath.Join(stagingRoot, item.ID)
	_, checksum, err := manager.ValidateAndExtractArchive(archivePath, stagingDir)
	if err != nil {
		return PendingRestore{}, err
	}
	if item.SHA256 != "" && !strings.EqualFold(item.SHA256, checksum) {
		_ = os.RemoveAll(stagingDir)
		return PendingRestore{}, errors.New("backup checksum mismatch")
	}
	marker := PendingRestore{
		BackupID:   item.ID,
		StagingDir: stagingDir,
		SHA256:     checksum,
		StagedAt:   time.Now().UTC(),
	}
	body, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return PendingRestore{}, err
	}
	if err := writeAtomic(PendingRestorePath(backupDir), body, 0o600); err != nil {
		_ = os.RemoveAll(stagingDir)
		return PendingRestore{}, fmt.Errorf("write restore marker: %w", err)
	}
	return marker, nil
}

func ApplyPendingRestore(options ApplyRestoreOptions) (ApplyRestoreResult, error) {
	markerPath := PendingRestorePath(options.BackupDir)
	body, err := os.ReadFile(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		return ApplyRestoreResult{}, nil
	}
	if err != nil {
		return ApplyRestoreResult{}, fmt.Errorf("read restore marker: %w", err)
	}
	var marker PendingRestore
	if err := json.Unmarshal(body, &marker); err != nil {
		return ApplyRestoreResult{}, fmt.Errorf("decode restore marker: %w", err)
	}
	stagingRoot := filepath.Join(options.BackupDir, "restore-staging")
	if marker.BackupID == "" || !pathWithinRoot(marker.StagingDir, stagingRoot) {
		return ApplyRestoreResult{}, errors.New("restore marker references an unsafe staging directory")
	}
	manifestBytes, err := os.ReadFile(filepath.Join(marker.StagingDir, "manifest.json"))
	if err != nil {
		return ApplyRestoreResult{}, fmt.Errorf("read staged manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return ApplyRestoreResult{}, fmt.Errorf("decode staged manifest: %w", err)
	}
	stagedDB := filepath.Join(marker.StagingDir, filepath.FromSlash(manifest.DatabaseFile))
	if err := validateSQLite(stagedDB); err != nil {
		return ApplyRestoreResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(options.DatabasePath), 0o750); err != nil {
		return ApplyRestoreResult{}, fmt.Errorf("prepare database directory: %w", err)
	}
	rollbackPath := ""
	if info, err := os.Stat(options.DatabasePath); err == nil && info.Mode().IsRegular() {
		rollbackPath = filepath.Join(options.BackupDir, "pre-restore-"+time.Now().UTC().Format("20060102T150405Z")+".db")
		if err := copyFile(options.DatabasePath, rollbackPath, 0o600); err != nil {
			return ApplyRestoreResult{}, fmt.Errorf("create pre-restore database copy: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ApplyRestoreResult{}, err
	}

	databaseTemp := options.DatabasePath + ".restore.tmp"
	_ = os.Remove(databaseTemp)
	if err := copyFile(stagedDB, databaseTemp, 0o600); err != nil {
		return ApplyRestoreResult{}, fmt.Errorf("stage restored database: %w", err)
	}
	if err := os.Rename(databaseTemp, options.DatabasePath); err != nil {
		_ = os.Remove(databaseTemp)
		return ApplyRestoreResult{}, fmt.Errorf("activate restored database: %w", err)
	}
	_ = os.Remove(options.DatabasePath + "-wal")
	_ = os.Remove(options.DatabasePath + "-shm")

	if err := restoreManagedDirectory(filepath.Join(marker.StagingDir, "nginx", "sites-available"), options.NginxSitesAvailable); err != nil {
		return ApplyRestoreResult{}, err
	}
	if err := restoreManagedDirectory(filepath.Join(marker.StagingDir, "nginx", "sites-enabled"), options.NginxSitesEnabled); err != nil {
		return ApplyRestoreResult{}, err
	}
	for _, project := range manifest.Projects {
		if !pathWithinConfiguredRoot(project.WorkDir, options.ProjectsRoot) {
			continue
		}
		for _, name := range project.Files {
			if filepath.Base(name) != name {
				return ApplyRestoreResult{}, errors.New("backup project manifest contains unsafe file name")
			}
			source := filepath.Join(marker.StagingDir, "projects", project.ID, name)
			if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err := copyFile(source, filepath.Join(project.WorkDir, name), 0o600); err != nil {
				return ApplyRestoreResult{}, fmt.Errorf("restore project config %s: %w", name, err)
			}
		}
	}

	if err := os.Remove(markerPath); err != nil {
		return ApplyRestoreResult{}, fmt.Errorf("remove restore marker: %w", err)
	}
	_ = os.RemoveAll(marker.StagingDir)
	return ApplyRestoreResult{Applied: true, BackupID: marker.BackupID, RollbackPath: rollbackPath}, nil
}

func restoreManagedDirectory(source, destination string) error {
	if strings.TrimSpace(destination) == "" {
		return nil
	}
	entries, err := os.ReadDir(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read staged managed directory: %w", err)
	}
	if err := os.MkdirAll(destination, 0o750); err != nil {
		return fmt.Errorf("prepare managed restore directory %s: %w", destination, err)
	}
	current, err := os.ReadDir(destination)
	if err != nil {
		return fmt.Errorf("list managed restore destination %s: %w", destination, err)
	}
	for _, entry := range current {
		if entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(destination, entry.Name())); err != nil {
			return fmt.Errorf("clear managed restore destination: %w", err)
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Base(entry.Name()) != entry.Name() {
			return errors.New("unsafe managed restore file name")
		}
		if err := copyFile(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name()), 0o640); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".devbox-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, input); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, destination)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".devbox-marker-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

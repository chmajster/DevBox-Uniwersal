package backups

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	archiveSchemaVersion = 1
	maxRestoreBytes      = int64(8 << 30)
)

var projectConfigNames = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
	"Dockerfile",
	".devbox.json",
}

type ManagerOptions struct {
	DatabasePath        string
	BackupDir           string
	ProjectsRoot        string
	NginxSitesAvailable string
	NginxSitesEnabled   string
	AppVersion          string
}

type Manager struct {
	db      *sql.DB
	options ManagerOptions
}

func NewManager(db *sql.DB, options ManagerOptions) *Manager {
	return &Manager{db: db, options: options}
}

func (m *Manager) BackupPath(fileName string) (string, error) {
	if fileName == "" || filepath.Base(fileName) != fileName {
		return "", errors.New("invalid control-plane backup file name")
	}
	root := filepath.Clean(m.options.BackupDir)
	path := filepath.Join(root, fileName)
	if !pathWithinRoot(path, root) {
		return "", errors.New("backup path escapes backup directory")
	}
	return path, nil
}

func (m *Manager) CreateArchive(ctx context.Context, item Backup) (int64, string, error) {
	finalPath, err := m.BackupPath(item.FileName)
	if err != nil {
		return 0, "", err
	}
	if err := os.MkdirAll(m.options.BackupDir, 0o700); err != nil {
		return 0, "", fmt.Errorf("create control-plane backup directory: %w", err)
	}
	workDir, err := os.MkdirTemp(m.options.BackupDir, ".backup-work-")
	if err != nil {
		return 0, "", fmt.Errorf("create backup workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	dbSnapshot := filepath.Join(workDir, "devbox.db")
	if err := m.snapshotDatabase(ctx, dbSnapshot); err != nil {
		return 0, "", err
	}

	manifest, projectFiles, err := m.buildManifest(ctx)
	if err != nil {
		return 0, "", err
	}
	tmpPath := finalPath + ".tmp"
	_ = os.Remove(tmpPath)
	if err := m.writeArchive(tmpPath, dbSnapshot, manifest, projectFiles); err != nil {
		_ = os.Remove(tmpPath)
		return 0, "", err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return 0, "", fmt.Errorf("chmod backup archive: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return 0, "", fmt.Errorf("activate backup archive: %w", err)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return 0, "", fmt.Errorf("stat backup archive: %w", err)
	}
	checksum, err := checksumFile(finalPath)
	if err != nil {
		return 0, "", err
	}
	return info.Size(), checksum, nil
}

func (m *Manager) snapshotDatabase(ctx context.Context, destination string) error {
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("prepare sqlite snapshot: %w", err)
	}
	if _, err := m.db.ExecContext(ctx, "VACUUM INTO ?", destination); err != nil {
		return fmt.Errorf("create sqlite snapshot: %w", err)
	}
	return nil
}

type projectFile struct {
	ProjectID string
	WorkDir   string
	Name      string
	Path      string
}

func (m *Manager) buildManifest(ctx context.Context) (Manifest, []projectFile, error) {
	manifest := Manifest{
		SchemaVersion:     archiveSchemaVersion,
		AppVersion:        m.options.AppVersion,
		CreatedAt:         time.Now().UTC(),
		RequiresMasterKey: true,
		DatabaseFile:      "state/devbox.db",
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, COALESCE(work_dir,'')
		FROM projects
		WHERE archived_at IS NULL
		ORDER BY id
	`)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("list projects for backup: %w", err)
	}
	defer rows.Close()

	var files []projectFile
	for rows.Next() {
		var id, workDir string
		if err := rows.Scan(&id, &workDir); err != nil {
			return Manifest{}, nil, fmt.Errorf("scan project for backup: %w", err)
		}
		workDir = strings.TrimSpace(workDir)
		if workDir == "" || !pathWithinConfiguredRoot(workDir, m.options.ProjectsRoot) {
			manifest.SkippedProjects = append(manifest.SkippedProjects, id)
			continue
		}
		artifact := ProjectArtifact{ID: id, WorkDir: filepath.Clean(workDir)}
		for _, name := range projectConfigNames {
			path := filepath.Join(workDir, name)
			info, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			artifact.Files = append(artifact.Files, name)
			files = append(files, projectFile{ProjectID: id, WorkDir: workDir, Name: name, Path: path})
		}
		manifest.Projects = append(manifest.Projects, artifact)
	}
	if err := rows.Err(); err != nil {
		return Manifest{}, nil, err
	}
	return manifest, files, nil
}

func (m *Manager) writeArchive(path, databaseSnapshot string, manifest Manifest, projectFiles []projectFile) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create backup archive: %w", err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)

	closeArchive := func() error {
		return errors.Join(tw.Close(), gz.Close(), file.Close())
	}
	fail := func(cause error) error {
		_ = closeArchive()
		return cause
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fail(fmt.Errorf("encode backup manifest: %w", err))
	}
	if err := addBytesToArchive(tw, "manifest.json", manifestBytes, 0o600); err != nil {
		return fail(err)
	}
	configSnapshot, _ := json.MarshalIndent(map[string]any{
		"app_version":           m.options.AppVersion,
		"projects_root":         m.options.ProjectsRoot,
		"nginx_sites_available": m.options.NginxSitesAvailable,
		"nginx_sites_enabled":   m.options.NginxSitesEnabled,
		"note":                  "Secret values and DEVBOX_MASTER_KEY are intentionally excluded.",
	}, "", "  ")
	if err := addBytesToArchive(tw, "config/settings.json", configSnapshot, 0o600); err != nil {
		return fail(err)
	}
	if err := addFileToArchive(tw, databaseSnapshot, "state/devbox.db"); err != nil {
		return fail(err)
	}
	if err := addDirectoryFiles(tw, m.options.NginxSitesAvailable, "nginx/sites-available"); err != nil {
		return fail(err)
	}
	if err := addDirectoryFiles(tw, m.options.NginxSitesEnabled, "nginx/sites-enabled"); err != nil {
		return fail(err)
	}
	for _, item := range projectFiles {
		name := filepath.ToSlash(filepath.Join("projects", item.ProjectID, item.Name))
		if err := addFileToArchive(tw, item.Path, name); err != nil {
			return fail(err)
		}
	}
	if err := closeArchive(); err != nil {
		return fmt.Errorf("finalize backup archive: %w", err)
	}
	return nil
}

func addDirectoryFiles(tw *tar.Writer, root, archiveRoot string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read backup directory %s: %w", root, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := filepath.ToSlash(filepath.Join(archiveRoot, entry.Name()))
		if err := addFileToArchive(tw, path, name); err != nil {
			return err
		}
	}
	return nil
}

func addFileToArchive(tw *tar.Writer, source, name string) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open backup source %s: %w", source, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat backup source %s: %w", source, err)
	}
	header := &tar.Header{
		Name:    filepath.ToSlash(name),
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write backup header %s: %w", name, err)
	}
	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("write backup source %s: %w", source, err)
	}
	return nil
}

func addBytesToArchive(tw *tar.Writer, name string, data []byte, mode int64) error {
	header := &tar.Header{Name: filepath.ToSlash(name), Mode: mode, Size: int64(len(data)), ModTime: time.Now().UTC()}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

func (m *Manager) ValidateAndExtractArchive(archivePath, stagingDir string) (Manifest, string, error) {
	checksum, err := checksumFile(archivePath)
	if err != nil {
		return Manifest{}, "", err
	}
	if err := os.RemoveAll(stagingDir); err != nil {
		return Manifest{}, "", fmt.Errorf("reset restore staging: %w", err)
	}
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return Manifest{}, "", fmt.Errorf("create restore staging: %w", err)
	}
	if err := extractArchive(archivePath, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return Manifest{}, "", err
	}
	manifestBytes, err := os.ReadFile(filepath.Join(stagingDir, "manifest.json"))
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return Manifest{}, "", errors.New("backup manifest is missing")
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		_ = os.RemoveAll(stagingDir)
		return Manifest{}, "", fmt.Errorf("decode backup manifest: %w", err)
	}
	if manifest.SchemaVersion != archiveSchemaVersion || manifest.DatabaseFile != "state/devbox.db" {
		_ = os.RemoveAll(stagingDir)
		return Manifest{}, "", errors.New("unsupported or invalid backup manifest")
	}
	databasePath := filepath.Join(stagingDir, "state", "devbox.db")
	if err := validateSQLite(databasePath); err != nil {
		_ = os.RemoveAll(stagingDir)
		return Manifest{}, "", err
	}
	return manifest, checksum, nil
}

func extractArchive(archivePath, stagingDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip backup: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read backup archive: %w", err)
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return errors.New("backup contains unsafe path")
		}
		if !allowedArchivePath(filepath.ToSlash(name)) {
			return fmt.Errorf("backup contains unsupported path %q", header.Name)
		}
		target := filepath.Join(stagingDir, name)
		if !pathWithinRoot(target, stagingDir) {
			return errors.New("backup path escapes restore staging")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += header.Size
			if header.Size < 0 || total > maxRestoreBytes {
				return errors.New("backup exceeds restore size limit")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(out, tr, header.Size)
			closeErr := out.Close()
			if copyErr != nil || closeErr != nil {
				return errors.Join(copyErr, closeErr)
			}
		default:
			return fmt.Errorf("backup contains unsupported entry type for %q", header.Name)
		}
	}
	return nil
}

func allowedArchivePath(name string) bool {
	if name == "manifest.json" || name == "config/settings.json" || name == "state/devbox.db" {
		return true
	}
	for _, prefix := range []string{"nginx/sites-available/", "nginx/sites-enabled/", "projects/"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func validateSQLite(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("backup sqlite database is missing")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open backup sqlite database: %w", err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("verify backup sqlite database: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(result)) != "ok" {
		return fmt.Errorf("backup sqlite integrity check failed: %s", result)
	}
	return nil
}

func checksumFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func pathWithinConfiguredRoot(path, root string) bool {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(pathAbs); err == nil {
		pathAbs = resolved
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}
	return pathWithinRoot(pathAbs, rootAbs)
}

func pathWithinRoot(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

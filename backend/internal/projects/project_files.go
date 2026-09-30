package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxProjectFileEntries = 1000

type ProjectFileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Kind       string    `json:"kind"`
	SizeBytes  int64     `json:"size_bytes"`
	ModifiedAt time.Time `json:"modified_at"`
	IsSymlink  bool      `json:"is_symlink,omitempty"`
}

type ProjectFileListing struct {
	Path      string             `json:"path"`
	Parent    string             `json:"parent,omitempty"`
	Entries   []ProjectFileEntry `json:"entries"`
	Truncated bool               `json:"truncated,omitempty"`
}

func (s *Service) BrowseProjectFiles(ctx context.Context, id, relativePath string) (ProjectFileListing, error) {
	project, err := s.repo.Get(ctx, id)
	if err != nil {
		return ProjectFileListing{}, err
	}
	if strings.TrimSpace(project.LocalPath) == "" {
		return ProjectFileListing{}, fmt.Errorf("%w: project source directory is not configured", ErrInvalidInput)
	}
	return browseProjectFiles(project.LocalPath, relativePath)
}

func browseProjectFiles(root, relativePath string) (ProjectFileListing, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return ProjectFileListing{}, fmt.Errorf("%w: project source directory is not configured", ErrInvalidInput)
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return ProjectFileListing{}, fmt.Errorf("%w: resolve project root: %v", ErrInvalidInput, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(absoluteRoot))
	if err != nil {
		return ProjectFileListing{}, classifyDirectoryBrowseError("resolve project root", err)
	}
	rootInfo, err := os.Stat(resolvedRoot)
	if err != nil {
		return ProjectFileListing{}, classifyDirectoryBrowseError("stat project root", err)
	}
	if !rootInfo.IsDir() {
		return ProjectFileListing{}, fmt.Errorf("%w: project source path is not a directory", ErrInvalidInput)
	}

	cleanRelative, err := normalizeProjectRelativePath(relativePath)
	if err != nil {
		return ProjectFileListing{}, err
	}

	current := resolvedRoot
	if cleanRelative != "" {
		current = filepath.Join(resolvedRoot, cleanRelative)
	}
	resolvedCurrent, err := filepath.EvalSymlinks(current)
	if err != nil {
		return ProjectFileListing{}, classifyDirectoryBrowseError("resolve project directory", err)
	}
	if !pathWithinRoot(resolvedCurrent, resolvedRoot) {
		return ProjectFileListing{}, fmt.Errorf("%w: path is outside project source directory", ErrDirectoryAccess)
	}
	currentInfo, err := os.Stat(resolvedCurrent)
	if err != nil {
		return ProjectFileListing{}, classifyDirectoryBrowseError("stat project directory", err)
	}
	if !currentInfo.IsDir() {
		return ProjectFileListing{}, fmt.Errorf("%w: path is not a directory", ErrInvalidInput)
	}

	dirEntries, err := os.ReadDir(resolvedCurrent)
	if err != nil {
		return ProjectFileListing{}, classifyDirectoryBrowseError("read project directory", err)
	}

	entries := make([]ProjectFileEntry, 0, min(len(dirEntries), maxProjectFileEntries+1))
	for _, entry := range dirEntries {
		childPath := filepath.Join(resolvedCurrent, entry.Name())
		resolvedChild, resolveErr := filepath.EvalSymlinks(childPath)
		if resolveErr != nil || !pathWithinRoot(resolvedChild, resolvedRoot) {
			continue
		}

		info, statErr := os.Stat(resolvedChild)
		if statErr != nil {
			continue
		}

		kind := "file"
		size := info.Size()
		if info.IsDir() {
			kind = "directory"
			size = 0
		}

		logicalPath := entry.Name()
		if cleanRelative != "" {
			logicalPath = filepath.Join(cleanRelative, entry.Name())
		}
		entries = append(entries, ProjectFileEntry{
			Name:       entry.Name(),
			Path:       filepath.ToSlash(logicalPath),
			Kind:       kind,
			SizeBytes:  size,
			ModifiedAt: info.ModTime().UTC(),
			IsSymlink:  entry.Type()&os.ModeSymlink != 0,
		})
		if len(entries) > maxProjectFileEntries {
			break
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "directory"
		}
		left := strings.ToLower(entries[i].Name)
		right := strings.ToLower(entries[j].Name)
		if left == right {
			return entries[i].Name < entries[j].Name
		}
		return left < right
	})

	truncated := len(entries) > maxProjectFileEntries
	if truncated {
		entries = entries[:maxProjectFileEntries]
	}

	parent := ""
	if cleanRelative != "" {
		parent = filepath.Dir(cleanRelative)
		if parent == "." {
			parent = ""
		}
		parent = filepath.ToSlash(parent)
	}

	return ProjectFileListing{
		Path:      filepath.ToSlash(cleanRelative),
		Parent:    parent,
		Entries:   entries,
		Truncated: truncated,
	}, nil
}

func normalizeProjectRelativePath(path string) (string, error) {
	if strings.ContainsRune(path, '\x00') || len(path) > 4096 {
		return "", fmt.Errorf("%w: invalid project path", ErrInvalidInput)
	}
	if path == "" || path == "." {
		return "", nil
	}
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", fmt.Errorf("%w: project path must be relative", ErrInvalidInput)
	}
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return "", fmt.Errorf("%w: parent directory traversal is not allowed", ErrInvalidInput)
		}
	}

	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("%w: project path must stay inside the project directory", ErrInvalidInput)
	}
	return clean, nil
}

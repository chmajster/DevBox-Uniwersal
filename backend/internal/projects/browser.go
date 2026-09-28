package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrDirectoryAccess = errors.New("directory access denied")

const maxDirectoryEntries = 500

type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type DirectoryListing struct {
	Path        string           `json:"path"`
	Parent      string           `json:"parent,omitempty"`
	Directories []DirectoryEntry `json:"directories"`
	Truncated   bool             `json:"truncated,omitempty"`
}

func (s *Service) BrowseDirectories(path string) (DirectoryListing, error) {
	return browseDirectories(path, s.directoryBrowseRoots)
}

func normalizeDirectoryBrowseRoots(projectsRoot string, configured []string) []string {
	candidates := make([]string, 0, len(configured)+1)
	if strings.TrimSpace(projectsRoot) != "" {
		candidates = append(candidates, projectsRoot)
	}
	candidates = append(candidates, configured...)

	seen := make(map[string]struct{}, len(candidates))
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.ContainsRune(candidate, '\x00') {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		absolute = filepath.Clean(absolute)
		if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
			absolute = resolved
		}
		if _, ok := seen[absolute]; ok {
			continue
		}
		seen[absolute] = struct{}{}
		roots = append(roots, absolute)
	}
	return roots
}

func browseDirectories(path string, roots []string) (DirectoryListing, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		directories := make([]DirectoryEntry, 0, len(roots))
		for _, root := range roots {
			resolved, err := filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			info, err := os.Stat(resolved)
			if err != nil || !info.IsDir() {
				continue
			}
			directories = append(directories, DirectoryEntry{Name: resolved, Path: resolved})
		}
		return DirectoryListing{Path: "", Directories: directories}, nil
	}
	if strings.ContainsRune(path, '\x00') || len(path) > 4096 {
		return DirectoryListing{}, fmt.Errorf("%w: invalid directory path", ErrInvalidInput)
	}
	if !filepath.IsAbs(path) {
		return DirectoryListing{}, fmt.Errorf("%w: directory path must be absolute", ErrInvalidInput)
	}

	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return DirectoryListing{}, classifyDirectoryBrowseError("resolve directory", err)
	}
	if !directoryPathAllowed(resolved, roots) {
		return DirectoryListing{}, fmt.Errorf("%w: path is outside configured browse roots", ErrDirectoryAccess)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return DirectoryListing{}, classifyDirectoryBrowseError("stat directory", err)
	}
	if !info.IsDir() {
		return DirectoryListing{}, fmt.Errorf("%w: path is not a directory", ErrInvalidInput)
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return DirectoryListing{}, classifyDirectoryBrowseError("read directory", err)
	}

	directories := make([]DirectoryEntry, 0)
	seen := make(map[string]struct{})
	truncated := false
	for _, entry := range entries {
		childPath := filepath.Join(resolved, entry.Name())
		childResolved, resolveErr := filepath.EvalSymlinks(childPath)
		if resolveErr != nil {
			continue
		}
		childInfo, statErr := os.Stat(childResolved)
		if statErr != nil || !childInfo.IsDir() || !directoryPathAllowed(childResolved, roots) {
			continue
		}
		if childResolved == resolved {
			continue
		}
		if _, ok := seen[childResolved]; ok {
			continue
		}
		seen[childResolved] = struct{}{}
		directories = append(directories, DirectoryEntry{Name: entry.Name(), Path: childResolved})
		if len(directories) >= maxDirectoryEntries {
			truncated = len(entries) > len(directories)
			break
		}
	}

	parent := filepath.Dir(resolved)
	if parent == resolved || !directoryPathAllowed(parent, roots) {
		parent = ""
	}
	for _, root := range roots {
		if samePath(resolved, root) {
			parent = ""
			break
		}
	}

	return DirectoryListing{
		Path:        resolved,
		Parent:      parent,
		Directories: directories,
		Truncated:   truncated,
	}, nil
}

func directoryPathAllowed(path string, roots []string) bool {
	for _, root := range roots {
		if pathWithinRoot(path, root) {
			return true
		}
	}
	return false
}

func pathWithinRoot(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func samePath(left, right string) bool {
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	if leftErr == nil {
		left = leftResolved
	}
	if rightErr == nil {
		right = rightResolved
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func classifyDirectoryBrowseError(operation string, err error) error {
	switch {
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%w: %s: %v", ErrDirectoryAccess, operation, err)
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: directory does not exist", ErrInvalidInput)
	default:
		return fmt.Errorf("%w: %s: %v", ErrInvalidInput, operation, err)
	}
}

package applications

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxApplicationDirectoryEntries = 500

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
	path = strings.TrimSpace(path)
	if path == "" {
		items := make([]DirectoryEntry, 0, len(s.allowedRoots))
		for _, root := range s.allowedRoots {
			resolved, err := filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			info, err := os.Stat(resolved)
			if err != nil || !info.IsDir() {
				continue
			}
			items = append(items, DirectoryEntry{Name: resolved, Path: resolved})
		}
		sortDirectoryEntries(items)
		return DirectoryListing{Directories: items}, nil
	}

	resolved, err := s.resolveBrowseDirectory(path)
	if err != nil {
		return DirectoryListing{}, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return DirectoryListing{}, classifyDirectoryError("read directory", err)
	}

	items := make([]DirectoryEntry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		childPath := filepath.Join(resolved, entry.Name())
		childResolved, err := filepath.EvalSymlinks(childPath)
		if err != nil {
			continue
		}
		info, err := os.Stat(childResolved)
		if err != nil || !info.IsDir() || !s.directoryPathAllowed(childResolved) || childResolved == resolved {
			continue
		}
		if _, exists := seen[childResolved]; exists {
			continue
		}
		seen[childResolved] = struct{}{}
		items = append(items, DirectoryEntry{Name: entry.Name(), Path: childResolved})
	}
	sortDirectoryEntries(items)

	truncated := false
	if len(items) > maxApplicationDirectoryEntries {
		items = items[:maxApplicationDirectoryEntries]
		truncated = true
	}

	parent := filepath.Dir(resolved)
	if parent == resolved || !s.directoryPathAllowed(parent) || s.isBrowseRoot(resolved) {
		parent = ""
	}
	return DirectoryListing{
		Path:        resolved,
		Parent:      parent,
		Directories: items,
		Truncated:   truncated,
	}, nil
}

func (s *Service) CreateDirectory(parent, name string) (DirectoryEntry, error) {
	parent = strings.TrimSpace(parent)
	name = strings.TrimSpace(name)
	if err := validateDirectoryName(name); err != nil {
		return DirectoryEntry{}, err
	}

	resolvedParent, err := s.resolveBrowseDirectory(parent)
	if err != nil {
		return DirectoryEntry{}, err
	}
	childPath := filepath.Join(resolvedParent, name)
	if _, err := os.Lstat(childPath); err == nil {
		return DirectoryEntry{}, fmt.Errorf("%w: directory already exists", ErrInvalidInput)
	} else if !errors.Is(err, os.ErrNotExist) {
		return DirectoryEntry{}, classifyDirectoryError("check target directory", err)
	}

	if err := os.Mkdir(childPath, 0o750); err != nil {
		return DirectoryEntry{}, classifyDirectoryError("create directory", err)
	}
	created, err := filepath.EvalSymlinks(childPath)
	if err != nil {
		_ = os.Remove(childPath)
		return DirectoryEntry{}, classifyDirectoryError("resolve created directory", err)
	}
	if !s.directoryPathAllowed(created) {
		_ = os.Remove(childPath)
		return DirectoryEntry{}, fmt.Errorf("%w: created directory is outside allowed roots", ErrDirectoryAccess)
	}
	return DirectoryEntry{Name: name, Path: created}, nil
}

func (s *Service) resolveBrowseDirectory(path string) (string, error) {
	path = strings.TrimSpace(path)
	if err := validateBrowsePath(path); err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%w: invalid directory path", ErrInvalidInput)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(absolute))
	if err != nil {
		return "", classifyDirectoryError("resolve directory", err)
	}
	if !s.directoryPathAllowed(resolved) {
		return "", fmt.Errorf("%w: path is outside configured browse roots", ErrDirectoryAccess)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", classifyDirectoryError("stat directory", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: path is not a directory", ErrInvalidInput)
	}
	return resolved, nil
}

func (s *Service) directoryPathAllowed(path string) bool {
	for _, root := range s.allowedRoots {
		if pathWithin(path, root) {
			return true
		}
	}
	return false
}

func (s *Service) isBrowseRoot(path string) bool {
	for _, root := range s.allowedRoots {
		if sameDirectoryPath(path, root) {
			return true
		}
	}
	return false
}

func validateBrowsePath(path string) error {
	if path == "" || len(path) > 4096 || strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("%w: invalid directory path", ErrInvalidInput)
	}
	normalized := strings.ReplaceAll(path, "\\", "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return fmt.Errorf("%w: parent traversal is not allowed", ErrInvalidInput)
		}
	}
	return nil
}

func validateDirectoryName(name string) error {
	if name == "" || len(name) > 255 || strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("%w: invalid directory name", ErrInvalidInput)
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\") || filepath.Base(name) != name {
		return fmt.Errorf("%w: directory name must be a single path segment", ErrInvalidInput)
	}
	return nil
}

func sortDirectoryEntries(items []DirectoryEntry) {
	sort.SliceStable(items, func(i, j int) bool {
		left := strings.ToLower(items[i].Name)
		right := strings.ToLower(items[j].Name)
		if left == right {
			return items[i].Name < items[j].Name
		}
		return left < right
	})
}

func sameDirectoryPath(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return filepath.Clean(leftPath) == filepath.Clean(rightPath)
}

func classifyDirectoryError(action string, err error) error {
	switch {
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%w: %s: %v", ErrDirectoryAccess, action, err)
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: %s: directory does not exist", ErrInvalidInput, action)
	default:
		return fmt.Errorf("%s: %w", action, err)
	}
}

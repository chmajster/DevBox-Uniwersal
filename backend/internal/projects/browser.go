package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrDirectoryAccess = errors.New("directory access denied")

type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type DirectoryListing struct {
	Path        string           `json:"path"`
	Parent      string           `json:"parent,omitempty"`
	Directories []DirectoryEntry `json:"directories"`
}

func (s *Service) BrowseDirectories(path string) (DirectoryListing, error) {
	return browseDirectories(path)
}

func browseDirectories(path string) (DirectoryListing, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = string(filepath.Separator)
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
	for _, entry := range entries {
		childPath := filepath.Join(resolved, entry.Name())
		isDirectory := entry.IsDir()
		if !isDirectory && entry.Type()&os.ModeSymlink != 0 {
			childInfo, statErr := os.Stat(childPath)
			isDirectory = statErr == nil && childInfo.IsDir()
		}
		if !isDirectory {
			continue
		}
		directories = append(directories, DirectoryEntry{
			Name: entry.Name(),
			Path: childPath,
		})
	}

	parent := filepath.Dir(resolved)
	if parent == resolved {
		parent = ""
	}
	return DirectoryListing{
		Path:        resolved,
		Parent:      parent,
		Directories: directories,
	}, nil
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

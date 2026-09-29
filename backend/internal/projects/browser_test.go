package projects

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBrowseDirectoriesReturnsDirectoriesOnly(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "not-a-directory.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	listing, err := browseDirectories(root, []string{root})
	if err != nil {
		t.Fatalf("browseDirectories() error = %v", err)
	}
	if listing.Path != root {
		t.Fatalf("listing path = %q, want %q", listing.Path, root)
	}
	if len(listing.Directories) != 2 {
		t.Fatalf("directories = %d, want 2", len(listing.Directories))
	}
	if listing.Directories[0].Name != "alpha" || listing.Directories[1].Name != "beta" {
		t.Fatalf("unexpected directory ordering: %#v", listing.Directories)
	}
	for _, entry := range listing.Directories {
		if !filepath.IsAbs(entry.Path) {
			t.Fatalf("directory path must be absolute: %q", entry.Path)
		}
	}
}

func TestBrowseDirectoriesListsOnlyConfiguredRootsWhenPathIsEmpty(t *testing.T) {
	root := t.TempDir()
	listing, err := browseDirectories("", []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != "" || len(listing.Directories) != 1 || listing.Directories[0].Path != root {
		t.Fatalf("unexpected roots listing: %#v", listing)
	}
}

func TestBrowseDirectoriesRejectsRelativePaths(t *testing.T) {
	root := t.TempDir()
	if _, err := browseDirectories("../relative", []string{root}); err == nil {
		t.Fatal("expected relative path rejection")
	}
}

func TestBrowseDirectoriesRejectsPathsOutsideConfiguredRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, err := browseDirectories(outside, []string{root}); !errors.Is(err, ErrDirectoryAccess) {
		t.Fatalf("expected ErrDirectoryAccess, got %v", err)
	}
}

func TestBrowseDirectoriesCanonicalizesDirectorySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require additional privileges on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	listing, err := browseDirectories(root, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range listing.Directories {
		if entry.Name == "link" {
			found = true
			if entry.Path != target {
				t.Fatalf("symlink path = %q, want canonical target %q", entry.Path, target)
			}
		}
	}
	if !found {
		t.Fatal("directory symlink was not exposed by the browser")
	}
}

func TestBrowseDirectoriesDoesNotExposeSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require additional privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	listing, err := browseDirectories(root, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listing.Directories {
		if entry.Name == "escape" {
			t.Fatal("symlink escaping configured root must not be exposed")
		}
	}
}

func TestSuggestDirectoriesFiltersRealChildDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Documents", "Downloads", "Other"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Document.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	suggestions, err := suggestDirectories(filepath.Join(root, "Doc"), []string{root}, 20)
	if err != nil {
		t.Fatalf("suggestDirectories() error = %v", err)
	}
	if len(suggestions.Items) != 1 {
		t.Fatalf("suggestions = %#v, want exactly one directory", suggestions.Items)
	}
	if suggestions.Items[0].Name != "Documents" || suggestions.Items[0].Path != filepath.Join(root, "Documents") {
		t.Fatalf("unexpected suggestion: %#v", suggestions.Items[0])
	}
}

func TestSuggestDirectoriesListsChildrenAfterTrailingSeparator(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	suggestions, err := suggestDirectories(root+string(filepath.Separator), []string{root}, 20)
	if err != nil {
		t.Fatalf("suggestDirectories() error = %v", err)
	}
	if len(suggestions.Items) != 2 {
		t.Fatalf("suggestions = %#v, want two directories", suggestions.Items)
	}
}

func TestSuggestDirectoriesCanCompleteConfiguredRootWithoutScanningItsParent(t *testing.T) {
	root := t.TempDir()
	if len(root) < 2 {
		t.Skip("temporary path is unexpectedly short")
	}
	query := root[:len(root)-1]

	suggestions, err := suggestDirectories(query, []string{root}, 20)
	if err != nil {
		t.Fatalf("suggestDirectories() error = %v", err)
	}
	if len(suggestions.Items) != 1 || suggestions.Items[0].Path != root {
		t.Fatalf("unexpected root suggestions: %#v", suggestions.Items)
	}
}

func TestSuggestDirectoriesRejectsParentTraversal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "child") + string(filepath.Separator) + ".." + string(filepath.Separator)
	if _, err := suggestDirectories(path, []string{root}, 20); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestSuggestDirectoriesDoesNotExposeSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require additional privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	suggestions, err := suggestDirectories(filepath.Join(root, "esc"), []string{root}, 20)
	if err != nil {
		t.Fatalf("suggestDirectories() error = %v", err)
	}
	if len(suggestions.Items) != 0 {
		t.Fatalf("symlink escaping configured root must not be suggested: %#v", suggestions.Items)
	}
}

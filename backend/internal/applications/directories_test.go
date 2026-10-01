package applications

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func directoryTestService(root string) *Service {
	return &Service{allowedRoots: normalizeRoots([]string{root})}
}

func TestBrowseDirectoriesListsAllowedChildrenCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta", "Alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	listing, err := directoryTestService(root).BrowseDirectories(root)
	if err != nil {
		t.Fatalf("BrowseDirectories() error = %v", err)
	}
	if len(listing.Directories) != 3 {
		t.Fatalf("directories = %#v, want 3", listing.Directories)
	}
	got := []string{listing.Directories[0].Name, listing.Directories[1].Name, listing.Directories[2].Name}
	want := []string{"Alpha", "beta", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("directory order = %#v, want %#v", got, want)
		}
	}
}

func TestBrowseDirectoriesReturnsConfiguredRoots(t *testing.T) {
	root := t.TempDir()
	listing, err := directoryTestService(root).BrowseDirectories("")
	if err != nil {
		t.Fatalf("BrowseDirectories() error = %v", err)
	}
	if len(listing.Directories) != 1 || listing.Directories[0].Path != root {
		t.Fatalf("configured roots = %#v, want %q", listing.Directories, root)
	}
}

func TestBrowseDirectoriesRejectsTraversalAndOutsideRoot(t *testing.T) {
	root := t.TempDir()
	service := directoryTestService(root)

	if _, err := service.BrowseDirectories(filepath.Join(root, "child", "..")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for traversal, got %v", err)
	}
	if _, err := service.BrowseDirectories(t.TempDir()); !errors.Is(err, ErrDirectoryAccess) {
		t.Fatalf("expected ErrDirectoryAccess outside root, got %v", err)
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

	listing, err := directoryTestService(root).BrowseDirectories(root)
	if err != nil {
		t.Fatalf("BrowseDirectories() error = %v", err)
	}
	for _, item := range listing.Directories {
		if item.Name == "escape" {
			t.Fatalf("symlink escaping allowed root was exposed: %#v", item)
		}
	}
	if _, err := directoryTestService(root).BrowseDirectories(filepath.Join(root, "escape")); !errors.Is(err, ErrDirectoryAccess) {
		t.Fatalf("expected ErrDirectoryAccess for symlink escape, got %v", err)
	}
}

func TestCreateDirectoryCreatesChildInsideAllowedRoot(t *testing.T) {
	root := t.TempDir()
	service := directoryTestService(root)

	entry, err := service.CreateDirectory(root, "Nowa Aplikacja")
	if err != nil {
		t.Fatalf("CreateDirectory() error = %v", err)
	}
	want := filepath.Join(root, "Nowa Aplikacja")
	if entry.Path != want || entry.Name != "Nowa Aplikacja" {
		t.Fatalf("created entry = %#v, want %q", entry, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("created path is not a directory")
	}
}

func TestCreateDirectoryRejectsInvalidNames(t *testing.T) {
	root := t.TempDir()
	service := directoryTestService(root)
	for _, name := range []string{"", ".", "..", "../escape", "nested/child", "nested\\child"} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CreateDirectory(root, name); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput for %q, got %v", name, err)
			}
		})
	}
}

func TestCreateDirectoryRejectsSymlinkParentOutsideRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require additional privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := directoryTestService(root).CreateDirectory(link, "blocked"); !errors.Is(err, ErrDirectoryAccess) {
		t.Fatalf("expected ErrDirectoryAccess, got %v", err)
	}
}

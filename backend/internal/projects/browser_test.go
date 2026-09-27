package projects

import (
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

	listing, err := browseDirectories(root)
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

func TestBrowseDirectoriesRejectsRelativePaths(t *testing.T) {
	if _, err := browseDirectories("../relative"); err == nil {
		t.Fatal("expected relative path rejection")
	}
}

func TestBrowseDirectoriesFollowsDirectorySymlink(t *testing.T) {
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

	listing, err := browseDirectories(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range listing.Directories {
		if entry.Name == "link" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("directory symlink was not exposed by the browser")
	}
}

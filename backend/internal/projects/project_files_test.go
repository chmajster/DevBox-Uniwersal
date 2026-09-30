package projects

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBrowseProjectFilesListsDirectoriesBeforeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	listing, err := browseProjectFiles(root, "")
	if err != nil {
		t.Fatalf("browseProjectFiles() error = %v", err)
	}
	if listing.Path != "" || listing.Parent != "" {
		t.Fatalf("unexpected root listing path: %#v", listing)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(listing.Entries))
	}
	if listing.Entries[0].Name != "src" || listing.Entries[0].Kind != "directory" {
		t.Fatalf("first entry = %#v, want src directory", listing.Entries[0])
	}
	if listing.Entries[1].Name != "README.md" || listing.Entries[1].Kind != "file" {
		t.Fatalf("second entry = %#v, want README.md file", listing.Entries[1])
	}
	if listing.Entries[1].SizeBytes != int64(len("hello\n")) {
		t.Fatalf("file size = %d", listing.Entries[1].SizeBytes)
	}
}

func TestBrowseProjectFilesNavigatesNestedDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "src", "app")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "main.go"), []byte("package main\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	listing, err := browseProjectFiles(root, "src/app")
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != "src/app" || listing.Parent != "src" {
		t.Fatalf("unexpected nested path: %#v", listing)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Path != "src/app/main.go" {
		t.Fatalf("unexpected nested entries: %#v", listing.Entries)
	}
}

func TestBrowseProjectFilesRejectsTraversalAndAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"../outside", "/tmp"} {
		if _, err := browseProjectFiles(root, path); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("path %q: expected ErrInvalidInput, got %v", path, err)
		}
	}
}

func TestBrowseProjectFilesDoesNotExposeSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation can require additional privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	listing, err := browseProjectFiles(root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range listing.Entries {
		if entry.Name == "escape" {
			t.Fatal("symlink escaping project root must not be exposed")
		}
	}
	if _, err := browseProjectFiles(root, "escape"); !errors.Is(err, ErrDirectoryAccess) {
		t.Fatalf("expected ErrDirectoryAccess when following escape, got %v", err)
	}
}

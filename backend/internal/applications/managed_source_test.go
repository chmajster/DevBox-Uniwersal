package applications

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestManagedSourceOwnershipDoesNotFollowLinks(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-only source preparation")
	}
	root := t.TempDir()
	directory := filepath.Join(root, "app")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "main.py")
	if err := os.WriteFile(file, []byte("print('app')"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "symlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(directory, "hardlink")); err != nil {
		t.Fatal(err)
	}
	s := &Service{projectsRoot: root}
	if err := s.prepareManagedSourceOwnership(context.Background(), directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{directory, file} {
		info, _ := os.Stat(name)
		if owner := info.Sys().(*syscall.Stat_t); owner.Uid != 10001 || owner.Gid != 10001 {
			t.Fatalf("managed source remains unwritable: %s %+v", name, owner)
		}
	}
	info, _ := os.Stat(outside)
	if info.Sys().(*syscall.Stat_t).Uid != 0 || info.Mode().Perm() != 0o600 {
		t.Fatal("outside source was changed")
	}
	link := filepath.Join(root, "escaped")
	if err := os.Symlink(filepath.Dir(outside), link); err != nil {
		t.Fatal(err)
	}
	if err := s.prepareManagedSourceOwnership(context.Background(), link); err == nil {
		t.Fatal("symlink source root was accepted")
	}
}

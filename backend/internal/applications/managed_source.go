package applications

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// A root-run control plane creates its private Git clones/starters as root.
// Give only those managed sources to the generated runtime's non-root user.
// Local sources, symlink targets, hard links and Git metadata are never changed.
func (s *Service) prepareManagedSourceOwnership(ctx context.Context, directory string) error {
	root, err := filepath.EvalSymlinks(s.projectsRoot)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !pathWithin(resolved, root) || resolved == root {
		return fmt.Errorf("%w: managed source must stay within its private source root", ErrInvalidInput)
	}
	if os.Geteuid() != 0 {
		return nil // The service account already owns the files it creates.
	}
	return filepath.WalkDir(directory, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return fs.SkipDir
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != 0 || (!info.IsDir() && (!info.Mode().IsRegular() || owner.Nlink > 1)) {
			return nil
		}
		if err := os.Lchown(name, 10001, 10001); err != nil {
			return fmt.Errorf("prepare managed source owner: %w", err)
		}
		return nil
	})
}

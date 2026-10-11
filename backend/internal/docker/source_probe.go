package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

// VerifyManagedSource checks the selected daemon, not just local path syntax.
// It proves host -> daemon and daemon -> host visibility before replacement.
// Only temporary probe files are touched; application code is never rewritten.
func (p *CLIProvider) VerifyManagedSource(ctx context.Context, spec containerspec.DeploymentSpec) error {
	if len(spec.BindMounts) == 0 {
		return nil // OCI applications have no source directory.
	}
	for source := range spec.BindMounts {
		if spec.Runtime == "custom" {
			if err := p.VerifyApplicationSource(ctx, source, spec.Labels); err != nil {
				return err
			}
			continue
		}
		if err := p.verifySource(ctx, spec, source); err != nil {
			return err
		}
	}
	return nil
}

// VerifyApplicationSource checks custom/Compose source mapping without assuming
// their application images contain a shell. Process-user compatibility remains
// controlled by the Dockerfile/Compose configuration.
func (p *CLIProvider) VerifyApplicationSource(ctx context.Context, source string, labels map[string]string) error {
	spec := containerspec.DeploymentSpec{Image: "alpine:3.22", BindMounts: map[string]string{source: "/devbox-source"}, Labels: labels}
	if _, err := managedMountArgs(spec); err != nil {
		return err
	}
	if info, err := os.Stat(source); err == nil {
		if owner, ok := info.Sys().(*syscall.Stat_t); ok {
			spec.User = strconv.FormatUint(uint64(owner.Uid), 10) + ":" + strconv.FormatUint(uint64(owner.Gid), 10)
		}
	}
	if err := p.EnsureImage(ctx, spec.Image); err != nil {
		return fmt.Errorf("source mapping probe image is unavailable: %w", err)
	}
	return p.verifySource(ctx, spec, source)
}

func (p *CLIProvider) verifySource(ctx context.Context, spec containerspec.DeploymentSpec, source string) error {
	if _, err := managedMountArgs(spec); err != nil {
		return err
	}
	file, err := os.CreateTemp(source, ".devbox-mount-probe-*")
	if err != nil {
		return fmt.Errorf("source is not writable by DevBox: %w; check source ownership and service permissions", err)
	}
	name := filepath.Base(file.Name())
	defer os.Remove(file.Name())
	defer os.Remove(file.Name() + ".container")
	if _, err := file.WriteString(name); err != nil {
		file.Close()
		return err
	}
	// DrvFS without Linux metadata can allow writes but reject chmod for the
	// service user. The container read below is the authoritative permission
	// check; chmod only makes this temporary nonce readable on POSIX filesystems.
	_ = file.Chmod(0o644)
	if err := file.Close(); err != nil {
		return err
	}
	user := spec.User
	if spec.Runtime == "php" {
		user = "33:33" // Apache's default www-data worker, never the root supervisor.
		if info, err := os.Stat(source); err == nil {
			if owner, ok := info.Sys().(*syscall.Stat_t); ok && owner.Uid != 0 {
				user = strconv.FormatUint(uint64(owner.Uid), 10) + ":" + strconv.FormatUint(uint64(owner.Gid), 10)
			}
		}
	}
	container := "devbox-source-probe-" + strings.TrimPrefix(name, ".devbox-mount-probe-")
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _, _ = p.runner.Run(cleanup, "container", "rm", "-f", container)
	}()
	args := []string{"container", "run", "--rm", "--name", container, "--network", "none", "--read-only", "--security-opt", "no-new-privileges:true", "--cap-drop", "ALL", "--entrypoint", "/bin/sh", "--mount", "type=bind,source=" + source + ",target=/devbox-source,readonly=false"}
	if user != "" {
		args = append(args, "--user", user)
	}
	for key, value := range spec.Labels {
		args = append(args, "--label", key+"="+value)
	}
	args = append(args, spec.Image, "-c", `test "$(cat "/devbox-source/$1")" = "$1" && (set -C; printf '%s' "$1" > "/devbox-source/$1.container")`, "devbox", name)
	probe, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, _, err := p.runner.Run(probe, args...); err != nil {
		return fmt.Errorf("Docker source mapping/RW probe failed for %s: %w; check the selected Docker daemon, WSL drive integration and source UID/GID permissions", source, err)
	}
	info, err := os.Lstat(file.Name() + ".container")
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Docker writes are not visible in the original source %s; check daemon/WSL path mapping", source)
	}
	data, err := os.ReadFile(file.Name() + ".container")
	if err != nil || string(data) != name {
		return fmt.Errorf("Docker source mapping does not match the original directory %s", source)
	}
	return nil
}

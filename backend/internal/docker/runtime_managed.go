package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func (p *CLIProvider) ManagedImageExists(ctx context.Context, image string) (bool, error) {
	if err := validateImageRef(image); err != nil {
		return false, err
	}
	_, _, err := p.runner.Run(ctx, "image", "inspect", image)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

func (p *CLIProvider) BuildManaged(ctx context.Context, spec containerspec.DeploymentSpec) error {
	if err := p.Available(ctx); err != nil {
		return err
	}
	if err := validateImageRef(spec.Image); err != nil {
		return err
	}
	if strings.TrimSpace(spec.ContextDir) == "" {
		return fmt.Errorf("%w: build context is required", ErrInvalidInput)
	}

	contextDir := spec.ContextDir
	dockerfile := spec.DockerfilePath
	cleanup := func() {}
	if spec.Dockerfile != "" {
		staged, err := stageManagedContext(spec.ContextDir)
		if err != nil {
			return err
		}
		cleanup = func() { _ = os.RemoveAll(staged) }
		contextDir = staged
		dockerfile = filepath.Join(staged, ".devbox.Dockerfile")
		if err := os.WriteFile(dockerfile, []byte(spec.Dockerfile), 0o600); err != nil {
			cleanup()
			return fmt.Errorf("write managed Dockerfile: %w", err)
		}
	}
	defer cleanup()
	if dockerfile == "" {
		return fmt.Errorf("%w: Dockerfile is required", ErrInvalidInput)
	}

	args := []string{"image", "build", "--pull", "--file", dockerfile, "--tag", spec.Image}
	labelKeys := make([]string, 0, len(spec.Labels))
	for key := range spec.Labels {
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)
	for _, key := range labelKeys {
		value := spec.Labels[key]
		if err := validateValue(key, "image label"); err != nil {
			return err
		}
		if err := validateValue(value, "image label value"); err != nil {
			return err
		}
		args = append(args, "--label", key+"="+value)
	}
	args = append(args, contextDir)
	_, _, err := p.runner.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("build managed image: %w", err)
	}
	return nil
}

func (p *CLIProvider) ReplaceManagedPorts(ctx context.Context, spec containerspec.DeploymentSpec, additional []providers.PublishedPort) (runErr error) {
	if err := p.Available(ctx); err != nil {
		return err
	}
	if err := validateContainerRef(spec.ContainerName); err != nil {
		return err
	}
	if err := validateImageRef(spec.Image); err != nil {
		return err
	}
	if spec.HostPort < 1 || spec.HostPort > 65535 || spec.ContainerPort < 1 || spec.ContainerPort > 65535 {
		return fmt.Errorf("%w: invalid managed container port", ErrInvalidInput)
	}
	publishArgs, err := managedPublishArgs(spec, additional)
	if err != nil {
		return err
	}

	args := []string{
		"container", "create",
		"--name", spec.ContainerName,
		"--restart", "unless-stopped",
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
	}
	for i, network := range spec.Networks {
		if err := validateNetworkRef(network); err != nil {
			return err
		}
		if i == 0 {
			args = append(args, "--network", network)
		}
	}
	extraHostArgs, err := managedExtraHostArgs(spec)
	if err != nil {
		return err
	}
	args = append(args, extraHostArgs...)
	args = append(args, publishArgs...)
	if spec.User != "" {
		if !regexp.MustCompile(`^[1-9][0-9]*:[1-9][0-9]*$`).MatchString(spec.User) {
			return fmt.Errorf("invalid non-root runtime UID:GID")
		}
		args = append(args, "--user", spec.User)
	}
	if spec.ReadOnly {
		args = append(args, "--read-only", "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=64m")
	}
	mountArgs, err := managedMountArgs(spec)
	if err != nil {
		return err
	}
	args = append(args, mountArgs...)
	labelKeys := make([]string, 0, len(spec.Labels))
	for key := range spec.Labels {
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)
	for _, key := range labelKeys {
		if err := validateValue(key, "container label"); err != nil {
			return err
		}
		if err := validateValue(spec.Labels[key], "container label value"); err != nil {
			return err
		}
		args = append(args, "--label", key+"="+spec.Labels[key])
	}
	envKeys := make([]string, 0, len(spec.Environment))
	for key := range spec.Environment {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		if err := validateEnvironmentName(key); err != nil {
			return err
		}
		value := spec.Environment[key]
		if err := validateValue(value, "environment value"); err != nil {
			return err
		}
		args = append(args, "--env", key+"="+value)
	}
	envFile, cleanupEnv, err := secureEnvFile(spec.SensitiveEnvironment)
	if err != nil {
		return err
	}
	defer cleanupEnv()
	if envFile != "" {
		args = append(args, "--env-file", envFile)
	}
	args = append(args, spec.Image)
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	backupName := spec.ContainerName + "-previous-" + hex.EncodeToString(suffix)
	previous, inspectErr := p.InspectContainer(ctx, spec.ContainerName)
	hadPrevious := inspectErr == nil
	if inspectErr != nil && !errors.Is(inspectErr, ErrNotFound) {
		return fmt.Errorf("inspect previous container: %w", inspectErr)
	}
	renamed, created, healthy := false, false, false
	defer func() {
		if recover() != nil {
			runErr = fmt.Errorf("managed replacement panicked; restoring previous container")
		}
		if healthy {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if created {
			if _, _, err := p.runner.Run(cleanup, "container", "rm", "-f", "-v", spec.ContainerName); err != nil && !errors.Is(err, ErrNotFound) {
				runErr = errors.Join(runErr, fmt.Errorf("remove failed replacement (previous retained as %s): %w", backupName, err))
				return
			}
		}
		if renamed {
			if _, _, err := p.runner.Run(cleanup, "container", "rename", backupName, spec.ContainerName); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("restore container name (retained as %s): %w", backupName, err))
				return
			}
			if previous.Running {
				if _, _, err := p.runner.Run(cleanup, "container", "start", spec.ContainerName); err != nil {
					runErr = errors.Join(runErr, fmt.Errorf("restart previous container: %w", err))
				}
			}
		}
	}()
	if hadPrevious {
		if _, _, err := p.runner.Run(ctx, "container", "rename", spec.ContainerName, backupName); err != nil {
			return fmt.Errorf("prepare replacement: %w", err)
		}
		renamed = true
		if previous.Running {
			if _, _, err := p.runner.Run(ctx, "container", "stop", "--time", "10", backupName); err != nil {
				return fmt.Errorf("stop previous container: %w", err)
			}
		}
	}
	if _, _, err := p.runner.Run(ctx, args...); err != nil {
		check, stop := context.WithTimeout(context.Background(), 5*time.Second)
		candidate, checkErr := p.InspectContainer(check, spec.ContainerName)
		stop()
		created = checkErr == nil && spec.Labels["io.devbox.project"] != "" && candidate.Image == spec.Image && candidate.Labels["io.devbox.project"] == spec.Labels["io.devbox.project"]
		return fmt.Errorf("create managed container: %w", err)
	}
	created = true
	for i, network := range spec.Networks {
		if i == 0 {
			continue
		}
		if err := p.ConnectNetwork(ctx, spec.ContainerName, network); err != nil {
			return fmt.Errorf("connect managed container network: %w", err)
		}
	}
	if _, _, err := p.runner.Run(ctx, "container", "start", spec.ContainerName); err != nil {
		return fmt.Errorf("start managed container: %w", err)
	}
	if err := providers.ReportDeploymentStage(ctx, "HEALTHCHECK"); err != nil {
		return err
	}
	if err := p.checkManagedAccess(ctx, spec); err != nil {
		return err
	}
	if err := p.waitManagedReady(ctx, spec.ContainerName, spec.HostPort, spec.Healthcheck); err != nil {
		return err
	}
	healthy = true
	if hadPrevious {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Cleanup cannot invalidate a healthy app. A unique name retains the
		// previous instance for manual cleanup if Docker removal fails.
		_, _, _ = p.runner.Run(cleanup, "container", "rm", "-f", "-v", backupName)
	}
	return nil
}

func managedExtraHostArgs(spec containerspec.DeploymentSpec) ([]string, error) {
	keys := make([]string, 0, len(spec.ExtraHosts))
	for host := range spec.ExtraHosts {
		keys = append(keys, host)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys)*2)
	for _, host := range keys {
		target := strings.TrimSpace(spec.ExtraHosts[host])
		if !strings.EqualFold(strings.TrimSpace(host), "host.docker.internal") || target != "host-gateway" {
			return nil, fmt.Errorf("%w: unsupported managed container host mapping", ErrInvalidInput)
		}
		args = append(args, "--add-host", "host.docker.internal:host-gateway")
	}
	return args, nil
}

func managedMountArgs(spec containerspec.DeploymentSpec) ([]string, error) {
	args := make([]string, 0, (len(spec.BindMounts)+len(spec.AnonymousVolumes))*2)
	sources := make([]string, 0, len(spec.BindMounts))
	for source := range spec.BindMounts {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		target := strings.TrimSpace(spec.BindMounts[source])
		if !filepath.IsAbs(source) {
			return nil, fmt.Errorf("%w: bind mount source must be absolute", ErrInvalidInput)
		}
		if strings.ContainsAny(source, "\x00\r\n,") {
			return nil, fmt.Errorf("%w: bind mount source contains unsupported characters", ErrInvalidInput)
		}
		if !strings.HasPrefix(target, "/") || strings.ContainsAny(target, "\x00\r\n,") {
			return nil, fmt.Errorf("%w: invalid bind mount target", ErrInvalidInput)
		}
		info, err := os.Stat(source)
		if err != nil {
			return nil, fmt.Errorf("%w: bind mount source is unavailable: %v", ErrInvalidInput, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: bind mount source must be a directory", ErrInvalidInput)
		}
		args = append(args, "--mount", "type=bind,source="+source+",target="+target)
	}

	volumes := append([]string(nil), spec.AnonymousVolumes...)
	sort.Strings(volumes)
	seen := map[string]struct{}{}
	for _, target := range volumes {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if _, duplicate := seen[target]; duplicate {
			continue
		}
		seen[target] = struct{}{}
		if !strings.HasPrefix(target, "/") || strings.ContainsAny(target, "\x00\r\n,") {
			return nil, fmt.Errorf("%w: invalid anonymous volume target", ErrInvalidInput)
		}
		args = append(args, "--mount", "type=volume,target="+target)
	}
	return args, nil
}

func (p *CLIProvider) RemoveManaged(ctx context.Context, containerName string) error {
	if err := validateContainerRef(containerName); err != nil {
		return err
	}
	_, _, err := p.runner.Run(ctx, "container", "rm", "-f", "-v", containerName)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (p *CLIProvider) waitManagedHealthy(ctx context.Context, containerName string, hostPort int) error {
	return p.waitManagedReady(ctx, containerName, hostPort, containerspec.Healthcheck{})
}

func stageManagedContext(source string) (string, error) {
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return "", fmt.Errorf("resolve managed context: %w", err)
	}
	target, err := os.MkdirTemp("", "devbox-runtime-context-*")
	if err != nil {
		return "", fmt.Errorf("create managed build context: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(target)
		}
	}()
	err = filepath.WalkDir(sourceAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if managedContextIgnored(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm()&0o755)
		if err != nil {
			_ = src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		srcErr := src.Close()
		dstErr := dst.Close()
		if copyErr != nil {
			return copyErr
		}
		if srcErr != nil {
			return srcErr
		}
		return dstErr
	})
	if err != nil {
		return "", fmt.Errorf("stage managed build context: %w", err)
	}
	ok = true
	return target, nil
}

func managedContextIgnored(rel string, isDir bool) bool {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		switch part {
		case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".idea", ".vscode":
			return true
		}
	}
	base := parts[len(parts)-1]
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	if !isDir {
		switch base {
		case "devbox.db", ".DS_Store":
			return true
		}
	}
	return false
}

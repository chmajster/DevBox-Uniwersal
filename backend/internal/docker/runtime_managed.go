package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
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

func (p *CLIProvider) ReplaceManaged(ctx context.Context, spec containerspec.DeploymentSpec) error {
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
	backupName := spec.ContainerName + "-previous"
	_, _, _ = p.runner.Run(ctx, "container", "rm", "-f", "-v", backupName)

	hadPrevious := false
	if _, _, err := p.runner.Run(ctx, "container", "rename", spec.ContainerName, backupName); err == nil {
		hadPrevious = true
		_, _, _ = p.runner.Run(ctx, "container", "stop", "--time", "10", backupName)
	} else if !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("prepare managed container replacement: %w", err)
	}

	rollback := func() {
		_, _, _ = p.runner.Run(context.Background(), "container", "rm", "-f", "-v", spec.ContainerName)
		if hadPrevious {
			_, _, _ = p.runner.Run(context.Background(), "container", "rename", backupName, spec.ContainerName)
			_, _, _ = p.runner.Run(context.Background(), "container", "start", spec.ContainerName)
		}
	}

	args := []string{
		"container", "create",
		"--name", spec.ContainerName,
		"--restart", "unless-stopped",
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
		"--publish", strconv.Itoa(spec.HostPort) + ":" + strconv.Itoa(spec.ContainerPort),
	}
	if spec.ReadOnly {
		args = append(args, "--read-only", "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=64m")
	}
	mountArgs, err := managedMountArgs(spec)
	if err != nil {
		rollback()
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
			rollback()
			return err
		}
		if err := validateValue(spec.Labels[key], "container label value"); err != nil {
			rollback()
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
			rollback()
			return err
		}
		value := spec.Environment[key]
		if err := validateValue(value, "environment value"); err != nil {
			rollback()
			return err
		}
		args = append(args, "--env", key+"="+value)
	}
	args = append(args, spec.Image)
	if _, _, err := p.runner.Run(ctx, args...); err != nil {
		rollback()
		return fmt.Errorf("create managed container: %w", err)
	}
	if _, _, err := p.runner.Run(ctx, "container", "start", spec.ContainerName); err != nil {
		rollback()
		return fmt.Errorf("start managed container: %w", err)
	}
	if err := p.waitManagedHealthy(ctx, spec.ContainerName, spec.HostPort); err != nil {
		rollback()
		return err
	}
	if hadPrevious {
		_, _, _ = p.runner.Run(ctx, "container", "rm", "-f", "-v", backupName)
	}
	return nil
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
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}
	target := "http://127.0.0.1:" + strconv.Itoa(hostPort) + "/"
	var lastErr error
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			if lastErr == nil {
				lastErr = errors.New("container did not become healthy")
			}
			return fmt.Errorf("managed container healthcheck failed: %w", lastErr)
		}
		out, _, err := p.runner.Run(ctx, "container", "inspect", "--format", "{{.State.Running}}", containerName)
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
			if reqErr == nil {
				resp, httpErr := client.Do(req)
				if httpErr == nil {
					_ = resp.Body.Close()
					if resp.StatusCode < 500 {
						return nil
					}
					lastErr = fmt.Errorf("HTTP status %d", resp.StatusCode)
				} else {
					lastErr = httpErr
				}
			}
		} else if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("container is not running")
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
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

package proxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type commandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

type NginxOptions struct {
	Binary         string
	SitesAvailable string
	SitesEnabled   string
}

type NginxProvider struct {
	options NginxOptions
	runner  commandRunner
}

var _ providers.ReverseProxyProvider = (*NginxProvider)(nil)

func NewNginxProvider(options NginxOptions) *NginxProvider {
	if strings.TrimSpace(options.Binary) == "" {
		options.Binary = "nginx"
	}
	return &NginxProvider{options: options, runner: execCommandRunner{}}
}

func (n *NginxProvider) Detect(ctx context.Context) (bool, error) {
	_, err := n.Version(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return false, nil
	}
	return false, err
}

func (n *NginxProvider) Version(ctx context.Context) (string, error) {
	out, err := n.runner.Run(ctx, n.options.Binary, "-v")
	if err != nil {
		return "", fmt.Errorf("nginx version: %w", err)
	}
	version := strings.TrimSpace(out)
	version = strings.TrimPrefix(version, "nginx version:")
	return strings.TrimSpace(version), nil
}

func (n *NginxProvider) Validate(ctx context.Context) error {
	out, err := n.runner.Run(ctx, n.options.Binary, "-t")
	if err != nil {
		return fmt.Errorf("nginx config validation failed: %s: %w", strings.TrimSpace(out), err)
	}
	return nil
}

func (n *NginxProvider) ValidateConfig(ctx context.Context) error {
	return n.Validate(ctx)
}

func (n *NginxProvider) Render(route providers.ProxyRoute) (string, error) {
	hostname, err := NormalizeHostname(route.Domain)
	if err != nil {
		return "", err
	}
	upstream, err := normalizeUpstream(route.Upstream)
	if err != nil {
		return "", err
	}
	if route.TLS {
		return "", fmt.Errorf("%w: TLS proxy sites require certificate integration and are not enabled by this module", ErrInvalidInput)
	}
	return fmt.Sprintf(`# Managed by DevBox Universal. Do not edit manually.
server {
    listen 80;
    listen [::]:80;
    server_name %s;

    location / {
        proxy_pass %s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_connect_timeout 5s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
}
`, hostname, upstream), nil
}

func (n *NginxProvider) TestRoute(ctx context.Context, route providers.ProxyRoute) error {
	rendered, err := n.Render(route)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "devbox-nginx-candidate-*")
	if err != nil {
		return fmt.Errorf("create nginx candidate directory: %w", err)
	}
	defer os.RemoveAll(tmp)

	pidPath := filepath.ToSlash(filepath.Join(tmp, "nginx.pid"))
	config := "worker_processes 1;\n" +
		"error_log stderr notice;\n" +
		"pid " + pidPath + ";\n" +
		"events {}\n" +
		"http {\naccess_log off;\n" + rendered + "\n}\n"
	candidate := filepath.Join(tmp, "nginx.conf")
	if err := os.WriteFile(candidate, []byte(config), 0o600); err != nil {
		return fmt.Errorf("write nginx candidate: %w", err)
	}
	out, err := n.runner.Run(ctx, n.options.Binary, "-t", "-c", candidate, "-p", tmp+string(os.PathSeparator))
	if err != nil {
		return fmt.Errorf("nginx candidate validation failed: %s: %w", strings.TrimSpace(out), err)
	}
	return nil
}

func (n *NginxProvider) CreateSite(ctx context.Context, route providers.ProxyRoute) error {
	return n.Apply(ctx, route)
}

func (n *NginxProvider) UpdateSite(ctx context.Context, route providers.ProxyRoute) error {
	return n.Apply(ctx, route)
}

func (n *NginxProvider) Apply(ctx context.Context, route providers.ProxyRoute) error {
	hostname, err := NormalizeHostname(route.Domain)
	if err != nil {
		return err
	}
	route.Domain = hostname
	rendered, err := n.Render(route)
	if err != nil {
		return err
	}
	if err := n.TestRoute(ctx, route); err != nil {
		return err
	}
	if err := ensureDirectory(n.options.SitesAvailable, "prepare Nginx sites-available"); err != nil {
		return err
	}
	if err := ensureDirectory(n.options.SitesEnabled, "prepare Nginx sites-enabled"); err != nil {
		return err
	}

	available := n.availablePath(hostname)
	enabled := n.enabledPath(hostname)
	oldAvailable, err := snapshot(available)
	if err != nil {
		return err
	}
	oldEnabled, err := snapshot(enabled)
	if err != nil {
		return err
	}

	if err := atomicWriteFile(available, []byte(rendered), 0o644); err != nil {
		return privilegeFailure("activate Nginx site", available, err)
	}
	if err := n.enableSite(available, enabled); err != nil {
		_ = restoreSnapshot(available, oldAvailable)
		return err
	}

	if err := n.Validate(ctx); err != nil {
		restoreErr := errors.Join(
			restoreSnapshot(available, oldAvailable),
			restoreSnapshot(enabled, oldEnabled),
		)
		return errors.Join(err, restoreErr)
	}
	if err := n.reloadOnly(ctx); err != nil {
		restoreErr := errors.Join(
			restoreSnapshot(available, oldAvailable),
			restoreSnapshot(enabled, oldEnabled),
		)
		if restoreErr == nil {
			if validationErr := n.Validate(ctx); validationErr == nil {
				_ = n.reloadOnly(ctx)
			}
		}
		return errors.Join(err, restoreErr)
	}
	return nil
}

func (n *NginxProvider) DisableSite(ctx context.Context, domain string) error {
	hostname, err := NormalizeHostname(domain)
	if err != nil {
		return err
	}
	enabled := n.enabledPath(hostname)
	oldEnabled, err := snapshot(enabled)
	if err != nil {
		return err
	}
	if !oldEnabled.exists {
		return ErrNotFound
	}
	if err := os.Remove(enabled); err != nil {
		return privilegeFailure("disable Nginx site", enabled, err)
	}
	if err := n.Validate(ctx); err != nil {
		return errors.Join(err, restoreSnapshot(enabled, oldEnabled))
	}
	if err := n.reloadOnly(ctx); err != nil {
		restoreErr := restoreSnapshot(enabled, oldEnabled)
		if restoreErr == nil {
			if validationErr := n.Validate(ctx); validationErr == nil {
				_ = n.reloadOnly(ctx)
			}
		}
		return errors.Join(err, restoreErr)
	}
	return nil
}

func (n *NginxProvider) DeleteSite(ctx context.Context, domain string) error {
	hostname, err := NormalizeHostname(domain)
	if err != nil {
		return err
	}
	available := n.availablePath(hostname)
	enabled := n.enabledPath(hostname)
	oldAvailable, err := snapshot(available)
	if err != nil {
		return err
	}
	oldEnabled, err := snapshot(enabled)
	if err != nil {
		return err
	}
	if !oldAvailable.exists && !oldEnabled.exists {
		return ErrNotFound
	}

	if err := removeIfExists(enabled); err != nil {
		return privilegeFailure("delete enabled Nginx site", enabled, err)
	}
	if err := removeIfExists(available); err != nil {
		_ = restoreSnapshot(enabled, oldEnabled)
		return privilegeFailure("delete Nginx site", available, err)
	}
	if err := n.Validate(ctx); err != nil {
		return errors.Join(err,
			restoreSnapshot(available, oldAvailable),
			restoreSnapshot(enabled, oldEnabled),
		)
	}
	if err := n.reloadOnly(ctx); err != nil {
		restoreErr := errors.Join(
			restoreSnapshot(available, oldAvailable),
			restoreSnapshot(enabled, oldEnabled),
		)
		if restoreErr == nil {
			if validationErr := n.Validate(ctx); validationErr == nil {
				_ = n.reloadOnly(ctx)
			}
		}
		return errors.Join(err, restoreErr)
	}
	return nil
}

func (n *NginxProvider) Remove(ctx context.Context, domain string) error {
	return n.DeleteSite(ctx, domain)
}

func (n *NginxProvider) Reload(ctx context.Context) error {
	if err := n.Validate(ctx); err != nil {
		return err
	}
	return n.reloadOnly(ctx)
}

func (n *NginxProvider) reloadOnly(ctx context.Context) error {
	out, err := n.runner.Run(ctx, n.options.Binary, "-s", "reload")
	if err != nil {
		return fmt.Errorf("nginx reload failed: %s: %w", strings.TrimSpace(out), err)
	}
	return nil
}

func (n *NginxProvider) availablePath(hostname string) string {
	return filepath.Join(n.options.SitesAvailable, hostname+".conf")
}

func (n *NginxProvider) enabledPath(hostname string) string {
	return filepath.Join(n.options.SitesEnabled, hostname+".conf")
}

func (n *NginxProvider) enableSite(available, enabled string) error {
	if filepath.Clean(available) == filepath.Clean(enabled) {
		return nil
	}
	if err := removeIfExists(enabled); err != nil {
		return privilegeFailure("replace enabled Nginx site", enabled, err)
	}
	if err := os.Symlink(available, enabled); err != nil {
		return privilegeFailure("enable Nginx site", enabled, err)
	}
	return nil
}

func normalizeUpstream(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: upstream is required", ErrInvalidInput)
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		value = "http://" + value
	}
	parts := strings.SplitN(value, "://", 2)
	if len(parts) != 2 || (parts[0] != "http" && parts[0] != "https") {
		return "", fmt.Errorf("%w: upstream scheme must be http or https", ErrInvalidInput)
	}
	host, portText, err := netSplitHostPort(parts[1])
	if err != nil || host == "" {
		return "", fmt.Errorf("%w: upstream must contain host and port", ErrInvalidInput)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("%w: upstream port is invalid", ErrInvalidInput)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", fmt.Errorf("%w: reverse proxy upstream must be local", ErrInvalidInput)
	}
	return parts[0] + "://" + formatHostPort(host, port), nil
}

func netSplitHostPort(value string) (string, string, error) {
	if strings.ContainsAny(value, "/?#") {
		return "", "", fmt.Errorf("upstream paths are not supported")
	}
	if strings.HasPrefix(value, "[") {
		end := strings.Index(value, "]")
		if end < 0 || end+2 > len(value) || value[end+1] != ':' {
			return "", "", fmt.Errorf("invalid bracketed host")
		}
		return value[1:end], value[end+2:], nil
	}
	i := strings.LastIndex(value, ":")
	if i <= 0 || i == len(value)-1 {
		return "", "", fmt.Errorf("missing port")
	}
	return value[:i], value[i+1:], nil
}

func formatHostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
}

func ensureDirectory(path, operation string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: Nginx site directory is not configured", ErrInvalidInput)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return privilegeFailure(operation, path, err)
	}
	return nil
}

func privilegeFailure(operation, path string, err error) error {
	if os.IsPermission(err) {
		return &PrivilegeError{
			Operation: operation,
			Path:      path,
			Instruction: "Grant the DevBox service account write access to " + path +
				" or perform the change through a privilege-separated system service.",
			Err: err,
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

type pathSnapshot struct {
	exists bool
	mode   os.FileMode
	data   []byte
	link   string
}

func snapshot(path string) (pathSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return pathSnapshot{}, nil
	}
	if err != nil {
		return pathSnapshot{}, fmt.Errorf("snapshot %s: %w", path, err)
	}
	s := pathSnapshot{exists: true, mode: info.Mode()}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return pathSnapshot{}, fmt.Errorf("read symlink %s: %w", path, err)
		}
		s.link = target
		return s, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return pathSnapshot{}, fmt.Errorf("read %s: %w", path, err)
	}
	s.data = data
	return s, nil
}

func restoreSnapshot(path string, s pathSnapshot) error {
	if err := removeIfExists(path); err != nil {
		return err
	}
	if !s.exists {
		return nil
	}
	if s.mode&os.ModeSymlink != 0 {
		if err := os.Symlink(s.link, path); err != nil {
			return fmt.Errorf("restore symlink %s: %w", path, err)
		}
		return nil
	}
	return atomicWriteFile(path, s.data, s.mode.Perm())
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".devbox-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrOperationNotAllowed = errors.New("privileged operation not allowed")

var allowedPackages = map[string]string{
	"git":      "git",
	"docker":   "docker.io",
	"nginx":    "nginx",
	"mysql":    "default-mysql-server",
	"php":      "php-cli",
	"composer": "composer",
	"python":   "python3",
	"pip":      "python3-pip",
	"go":       "golang-go",
	"node":     "nodejs",
	"npm":      "npm",
}

var allowedServices = map[string]string{
	"devbox":  "devbox.service",
	"docker":  "docker.service",
	"nginx":   "nginx.service",
	"mysql":   "mysql.service",
	"mariadb": "mariadb.service",
}

var allowedEnvKeys = map[string]struct{}{
	"DEVBOX_HTTP_ADDR":                {},
	"DEVBOX_DATABASE_PATH":            {},
	"DEVBOX_MIGRATIONS_DIR":           {},
	"DEVBOX_FRONTEND_DIR":             {},
	"DEVBOX_COOKIE_SECURE":            {},
	"DEVBOX_VERSION":                  {},
	"DEVBOX_CONTROL_PLANE_BACKUP_DIR": {},
	"DEVBOX_NGINX_SITES_AVAILABLE":    {},
	"DEVBOX_NGINX_SITES_ENABLED":      {},
	"DEVBOX_PRIVILEGED_HELPER":        {},
	"DEVBOX_SUDO_BINARY":              {},
}

type PrivilegedHelper struct {
	runner CommandRunner
}

func NewPrivilegedHelper() *PrivilegedHelper {
	return &PrivilegedHelper{runner: execRunner{}}
}

func NewPrivilegedHelperWithRunner(runner CommandRunner) *PrivilegedHelper {
	return &PrivilegedHelper{runner: runner}
}

func (h *PrivilegedHelper) InstallPackage(ctx context.Context, component string) error {
	pkg, ok := allowedPackages[component]
	if !ok {
		return fmt.Errorf("%w: package %q", ErrOperationNotAllowed, component)
	}
	return h.run(ctx, "apt-get", "install", "-y", "--", pkg)
}

func (h *PrivilegedHelper) RestartService(ctx context.Context, service string) error {
	unit, ok := allowedServices[service]
	if !ok {
		return fmt.Errorf("%w: service %q", ErrOperationNotAllowed, service)
	}
	return h.run(ctx, "systemctl", "restart", unit)
}

func (h *PrivilegedHelper) ValidateNginx(ctx context.Context) error {
	if err := h.run(ctx, "nginx", "-t"); err != nil {
		return fmt.Errorf("validate nginx config: %w", err)
	}
	return nil
}

func (h *PrivilegedHelper) ReloadNginx(ctx context.Context) error {
	if err := h.ValidateNginx(ctx); err != nil {
		return err
	}
	return h.run(ctx, "systemctl", "reload", "nginx.service")
}

// WriteControlledConfig only writes the DevBox environment file and validates
// every key. It deliberately cannot write arbitrary paths or shell fragments.
func (h *PrivilegedHelper) WriteControlledConfig(name string, data []byte) error {
	if name != "devbox-env" {
		return fmt.Errorf("%w: config %q", ErrOperationNotAllowed, name)
	}
	if len(data) > 64*1024 {
		return errors.New("config too large")
	}
	if err := ValidateDevBoxEnv(string(data)); err != nil {
		return err
	}
	path := "/etc/devbox/devbox.env"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".devbox.env.*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o640); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("activate config: %w", err)
	}
	return nil
}

var safeEnvValue = regexp.MustCompile(`^[A-Za-z0-9_./:@,+\-]*$`)

func ValidateDevBoxEnv(raw string) error {
	for lineNo, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("invalid environment line %d", lineNo+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if _, ok := allowedEnvKeys[key]; !ok {
			return fmt.Errorf("%w: environment key %q", ErrOperationNotAllowed, key)
		}
		if !safeEnvValue.MatchString(value) {
			return fmt.Errorf("unsafe environment value for %s", key)
		}
	}
	return nil
}

func (h *PrivilegedHelper) run(ctx context.Context, command string, args ...string) error {
	path, err := h.runner.LookPath(command)
	if err != nil {
		return fmt.Errorf("find %s: %w", command, err)
	}
	out, err := h.runner.CombinedOutput(ctx, path, args...)
	if err != nil {
		message := ParseVersionOutput(string(out))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s failed: %s", command, message)
	}
	return nil
}

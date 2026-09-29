package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrOperationNotAllowed = errors.New("privileged operation not allowed")

var allowedPackages = map[string]string{"git": "git", "docker": "docker.io", "nginx": "nginx", "mysql": "default-mysql-server", "postgresql": "postgresql", "php": "php-cli", "php-fpm": "php-fpm", "php-ext-curl": "php-curl", "php-ext-mbstring": "php-mbstring", "php-ext-xml": "php-xml", "php-ext-zip": "php-zip", "php-ext-gd": "php-gd", "php-ext-intl": "php-intl", "php-ext-mysql": "php-mysql", "php-ext-pgsql": "php-pgsql", "php-ext-sqlite3": "php-sqlite3", "php-ext-bcmath": "php-bcmath", "php-ext-soap": "php-soap", "php-ext-ldap": "php-ldap", "php-ext-gmp": "php-gmp", "php-ext-imagick": "php-imagick", "php-ext-redis": "php-redis", "php-ext-memcached": "php-memcached", "php-ext-opcache": "php-opcache", "php-ext-xdebug": "php-xdebug", "composer": "composer", "python": "python3", "pip": "python3-pip", "go": "golang-go", "node": "nodejs", "npm": "npm"}

var allowedServices = map[string]string{
	"devbox":     "devbox.service",
	"docker":     "docker.service",
	"nginx":      "nginx.service",
	"mysql":      "mysql.service",
	"mariadb":    "mariadb.service",
	"postgresql": "postgresql.service",
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
	"DEVBOX_UPDATE_REPOSITORY":        {},
	"DEVBOX_UPDATE_REF":               {},
	"DEVBOX_UPDATE_PROGRESS_FILE":     {},
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
	if component == "docker-compose" {
		ok = true
	}
	if !ok {
		return fmt.Errorf("%w: package %q", ErrOperationNotAllowed, component)
	}

	// Package metadata can be absent or stale on fresh Debian/Ubuntu/WSL
	// installations. Refresh it before installing a whitelisted component.
	if err := h.runWithEnv(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get", "update"); err != nil {
		return fmt.Errorf("refresh apt package lists: %w", err)
	}
	if component == "docker-compose" {
		var err error
		pkg, err = h.firstAvailablePackage(ctx, []string{"docker-compose-v2", "docker-compose-plugin", "docker-compose"})
		if err != nil {
			return fmt.Errorf("resolve Docker Compose package: %w", err)
		}
	}
	if err := h.runWithEnv(ctx, []string{"DEBIAN_FRONTEND=noninteractive"}, "apt-get",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		"install", "-y", "--no-install-recommends", "--", pkg); err != nil {
		return fmt.Errorf("install %s (%s): %w", component, pkg, err)
	}
	return nil
}

func (h *PrivilegedHelper) firstAvailablePackage(ctx context.Context, candidates []string) (string, error) {
	aptCache, err := h.runner.LookPath("apt-cache")
	if err != nil {
		return "", fmt.Errorf("find apt-cache: %w", err)
	}
	for _, candidate := range candidates {
		out, checkErr := h.runner.CombinedOutput(ctx, aptCache, "show", candidate)
		if checkErr == nil && len(strings.TrimSpace(string(out))) > 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("none of the allowed packages are available: %s", strings.Join(candidates, ", "))
}

func (h *PrivilegedHelper) RestartService(ctx context.Context, service string) error {
	unit, ok := allowedServices[service]
	if !ok {
		return fmt.Errorf("%w: service %q", ErrOperationNotAllowed, service)
	}
	return h.run(ctx, "systemctl", "restart", unit)
}

func (h *PrivilegedHelper) ConfigureApplicationDatabase(ctx context.Context, engine string, port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("%w: invalid application database port %d", ErrOperationNotAllowed, port)
	}
	engine = strings.ToLower(strings.TrimSpace(engine))
	var script string
	switch engine {
	case "mysql", "mariadb":
		script = mysqlApplicationDatabaseScript(port)
	case "postgresql", "postgres":
		script = postgreSQLApplicationDatabaseScript(port)
	default:
		return fmt.Errorf("%w: application database engine %q", ErrOperationNotAllowed, engine)
	}

	if _, err := h.runner.LookPath("systemd-run"); err == nil {
		return h.run(ctx, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "/bin/sh", "-c", script)
	}
	return h.run(ctx, "sh", "-c", script)
}

func mysqlApplicationDatabaseScript(port int) string {
	return fmt.Sprintf(`set -eu
install -d -m 0755 /etc/mysql/conf.d
cat > /etc/mysql/conf.d/99-devbox-application.cnf <<'EOF'
# Managed by DevBox Universal.
# This host database is reserved for application workloads; DevBox control-plane data does not use it.
[mysqld]
port=%d
bind-address=0.0.0.0
EOF
chmod 0644 /etc/mysql/conf.d/99-devbox-application.cnf
if command -v systemctl >/dev/null 2>&1; then
  if systemctl cat mysql.service >/dev/null 2>&1; then
    systemctl enable --now mysql.service
    systemctl restart mysql.service
  elif systemctl cat mariadb.service >/dev/null 2>&1; then
    systemctl enable --now mariadb.service
    systemctl restart mariadb.service
  fi
fi
`, port)
}

func postgreSQLApplicationDatabaseScript(port int) string {
	return fmt.Sprintf(`set -eu
command -v pg_lsclusters >/dev/null 2>&1
command -v pg_conftool >/dev/null 2>&1
set -- $(pg_lsclusters --no-header | awk 'NR==1 {print $1, $2; exit}')
[ "$#" -eq 2 ]
version="$1"
cluster="$2"
pg_conftool "$version" "$cluster" set port %d
pg_conftool "$version" "$cluster" set listen_addresses '*'
hba="/etc/postgresql/$version/$cluster/pg_hba.conf"
marker="# DevBox application containers"
if ! grep -Fq "$marker" "$hba"; then
  cat >> "$hba" <<'EOF'

# DevBox application containers
host all all 172.16.0.0/12 scram-sha-256
host all all 10.0.0.0/8 scram-sha-256
host all all 192.168.0.0/16 scram-sha-256
EOF
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl enable --now postgresql.service
  systemctl restart postgresql.service
else
  pg_ctlcluster "$version" "$cluster" restart
fi
`, port)
}

func ParseApplicationDatabasePort(value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1024 || port > 65535 {
		return 0, fmt.Errorf("%w: invalid application database port", ErrOperationNotAllowed)
	}
	return port, nil
}

func (h *PrivilegedHelper) ValidateNginx(ctx context.Context) error {
	nginxPath, err := h.runner.LookPath("nginx")
	if err != nil {
		return fmt.Errorf("find nginx: %w", err)
	}

	// devbox.service is intentionally sandboxed with ProtectSystem=strict.
	// A sudo child keeps that mount namespace, so a direct "nginx -t" may fail
	// while probing /run/nginx.pid even though the real nginx.service can use it.
	// Run validation in a transient systemd unit so it executes in the manager's
	// namespace instead of inheriting the DevBox filesystem sandbox.
	if _, lookupErr := h.runner.LookPath("systemd-run"); lookupErr == nil {
		if err := h.run(ctx, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", nginxPath, "-t"); err != nil {
			return fmt.Errorf("validate nginx config: %w", err)
		}
		return nil
	}

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

func (h *PrivilegedHelper) StartUpdate(ctx context.Context) error {
	return h.run(ctx, "systemctl", "start", "--no-block", "devbox-update.service")
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
	return h.runWithEnv(ctx, nil, command, args...)
}

func (h *PrivilegedHelper) runWithEnv(ctx context.Context, env []string, command string, args ...string) error {
	path, err := h.runner.LookPath(command)
	if err != nil {
		return fmt.Errorf("find %s: %w", command, err)
	}

	// CommandRunner intentionally has a narrow interface. For apt, use env(1)
	// to set non-interactive mode without expanding the privileged helper API.
	execPath := path
	execArgs := args
	if len(env) > 0 {
		envPath, lookupErr := h.runner.LookPath("env")
		if lookupErr != nil {
			return fmt.Errorf("find env: %w", lookupErr)
		}
		execPath = envPath
		execArgs = append(append(append([]string{}, env...), path), args...)
	}

	var out []byte
	for attempt := 1; attempt <= 3; attempt++ {
		out, err = h.runner.CombinedOutput(ctx, execPath, execArgs...)
		if err == nil {
			return nil
		}
		message := strings.TrimSpace(string(out))
		lower := strings.ToLower(message)
		transient := strings.Contains(lower, "could not get lock") ||
			strings.Contains(lower, "unable to acquire the dpkg frontend lock") ||
			strings.Contains(lower, "temporary failure resolving") ||
			strings.Contains(lower, "failed to fetch")
		if !transient || attempt == 3 {
			if message == "" {
				message = err.Error()
			}
			return fmt.Errorf("%s failed: %s", command, message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt*2) * time.Second):
		}
	}
	return nil
}

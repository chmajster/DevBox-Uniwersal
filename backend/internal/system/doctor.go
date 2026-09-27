package system

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CheckStatus string

const (
	CheckOK   CheckStatus = "ok"
	CheckInfo CheckStatus = "info"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
)

type CheckResult struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"status"`
	Message string      `json:"message"`
}

type DoctorReport struct {
	Healthy bool          `json:"healthy"`
	Checks  []CheckResult `json:"checks"`
}

type DoctorOptions struct {
	DB            *sql.DB
	DatabasePath  string
	MigrationsDir string
	DataDir       string
	HTTPAddr      string
	ServiceName   string
	Runner        CommandRunner
}

func RunDoctor(ctx context.Context, opts DoctorOptions) DoctorReport {
	if opts.Runner == nil {
		opts.Runner = execRunner{}
	}
	if opts.ServiceName == "" {
		opts.ServiceName = "devbox"
	}
	checks := []CheckResult{
		checkSQLite(ctx, opts.DB, opts.DatabasePath),
		checkMigrations(ctx, opts.DB, opts.MigrationsDir),
		checkFilesystem(opts.DataDir),
		checkNginx(ctx, opts.Runner),
		checkDocker(ctx, opts.Runner),
		checkMySQL(ctx, opts.Runner),
		checkRuntimes(ctx, opts.Runner),
		checkHTTPPort(ctx, opts.HTTPAddr),
		checkDevBoxService(ctx, opts.Runner, opts.ServiceName),
	}
	return DoctorReport{Healthy: reportHealthy(checks), Checks: checks}
}

func reportHealthy(checks []CheckResult) bool {
	for _, check := range checks {
		if check.Status == CheckFail {
			return false
		}
	}
	return true
}

func checkSQLite(ctx context.Context, db *sql.DB, path string) CheckResult {
	if db == nil {
		return CheckResult{Name: "sqlite", Status: CheckFail, Message: "database is not open"}
	}
	if err := db.PingContext(ctx); err != nil {
		return CheckResult{Name: "sqlite", Status: CheckFail, Message: err.Error()}
	}
	return CheckResult{Name: "sqlite", Status: CheckOK, Message: path}
}

func checkMigrations(ctx context.Context, db *sql.DB, dir string) CheckResult {
	if db == nil {
		return CheckResult{Name: "migrations", Status: CheckFail, Message: "database is not open"}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return CheckResult{Name: "migrations", Status: CheckFail, Message: err.Error()}
	}
	var expected []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			expected = append(expected, entry.Name())
		}
	}
	sort.Strings(expected)
	for _, migration := range expected {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(1) FROM schema_migrations WHERE version = ?", migration).Scan(&count); err != nil {
			return CheckResult{Name: "migrations", Status: CheckFail, Message: err.Error()}
		}
		if count == 0 {
			return CheckResult{Name: "migrations", Status: CheckFail, Message: "pending migration: " + migration}
		}
	}
	return CheckResult{Name: "migrations", Status: CheckOK, Message: fmt.Sprintf("%d migration(s) applied", len(expected))}
}

func checkFilesystem(dataDir string) CheckResult {
	if dataDir == "" {
		dataDir = "."
	}
	info, err := os.Stat(dataDir)
	if err != nil {
		return CheckResult{Name: "filesystem", Status: CheckFail, Message: err.Error()}
	}
	if !info.IsDir() {
		return CheckResult{Name: "filesystem", Status: CheckFail, Message: dataDir + " is not a directory"}
	}
	file, err := os.CreateTemp(dataDir, ".doctor-*")
	if err != nil {
		return CheckResult{Name: "filesystem", Status: CheckFail, Message: "data directory is not writable: " + err.Error()}
	}
	name := file.Name()
	file.Close()
	_ = os.Remove(name)
	return CheckResult{Name: "filesystem", Status: CheckOK, Message: dataDir + " is writable"}
}

func checkNginx(ctx context.Context, runner CommandRunner) CheckResult {
	path, err := runner.LookPath("nginx")
	if err != nil {
		return CheckResult{Name: "nginx", Status: CheckWarn, Message: "nginx is not installed"}
	}
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	out, versionErr := runner.CombinedOutput(versionCtx, path, "-v")
	cancel()
	version := ParseVersionOutput(string(out))
	service := checkServiceAny(ctx, runner, "nginx", []string{"nginx.service"})
	if service.Status != CheckOK {
		if version != "" {
			service.Message = version + "; " + service.Message
		}
		return service
	}
	if versionErr != nil {
		return CheckResult{Name: "nginx", Status: CheckWarn, Message: firstDiagnosticLine(out, versionErr)}
	}
	if version != "" {
		service.Message = version + "; " + service.Message
	}
	return service
}

func checkDocker(ctx context.Context, runner CommandRunner) CheckResult {
	path, err := runner.LookPath("docker")
	if err != nil {
		return CheckResult{Name: "docker", Status: CheckWarn, Message: "docker is not installed"}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := runner.CombinedOutput(checkCtx, path, "info", "--format", "{{.ServerVersion}}")
	if err != nil {
		return CheckResult{Name: "docker", Status: CheckWarn, Message: firstDiagnosticLine(out, err)}
	}
	return CheckResult{Name: "docker", Status: CheckOK, Message: "daemon " + ParseVersionOutput(string(out))}
}

func checkMySQL(ctx context.Context, runner CommandRunner) CheckResult {
	if _, err := runner.LookPath("mysql"); err != nil {
		if _, mariaErr := runner.LookPath("mariadb"); mariaErr != nil {
			return CheckResult{Name: "mysql", Status: CheckWarn, Message: "mysql/mariadb client is not installed"}
		}
	}
	return checkServiceAny(ctx, runner, "mysql", []string{"mysql.service", "mariadb.service"})
}

func checkRuntimes(ctx context.Context, runner CommandRunner) CheckResult {
	statuses := DetectComponentsWithRunner(ctx, runner)
	var available []string
	for _, status := range statuses {
		switch status.Name {
		case "php", "python", "go", "node":
			if status.Installed && status.State != "error" {
				available = append(available, status.Name)
			}
		}
	}
	if len(available) == 0 {
		return CheckResult{Name: "runtimes", Status: CheckWarn, Message: "no application runtime detected"}
	}
	sort.Strings(available)
	return CheckResult{Name: "runtimes", Status: CheckOK, Message: strings.Join(available, ", ")}
}

func checkHTTPPort(ctx context.Context, addr string) CheckResult {
	if addr == "" {
		return CheckResult{Name: "ports", Status: CheckWarn, Message: "DEVBOX_HTTP_ADDR is empty"}
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return CheckResult{Name: "ports", Status: CheckFail, Message: err.Error()}
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, port) + "/api/v1/health"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return CheckResult{Name: "ports", Status: CheckWarn, Message: addr + " is not accepting DevBox health requests"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CheckResult{Name: "ports", Status: CheckFail, Message: fmt.Sprintf("%s returned HTTP %d", addr, resp.StatusCode)}
	}
	return CheckResult{Name: "ports", Status: CheckOK, Message: addr + " serves DevBox health"}
}

func checkDevBoxService(ctx context.Context, runner CommandRunner, service string) CheckResult {
	return checkServiceAny(ctx, runner, "devbox-service", []string{service + ".service"})
}

func checkServiceAny(ctx context.Context, runner CommandRunner, name string, units []string) CheckResult {
	systemctl, err := runner.LookPath("systemctl")
	if err != nil {
		return CheckResult{Name: name, Status: CheckWarn, Message: "systemctl is unavailable"}
	}
	var diagnostics []string
	for _, unit := range units {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		out, err := runner.CombinedOutput(checkCtx, systemctl, "is-active", unit)
		cancel()
		state := ParseVersionOutput(string(out))
		if err == nil && state == "active" {
			return CheckResult{Name: name, Status: CheckOK, Message: unit + " is active"}
		}
		if state != "" {
			diagnostics = append(diagnostics, unit+"="+state)
		}
	}
	if len(diagnostics) == 0 {
		diagnostics = append(diagnostics, "service is not active")
	}
	return CheckResult{Name: name, Status: CheckWarn, Message: strings.Join(diagnostics, ", ")}
}

func firstDiagnosticLine(out []byte, err error) string {
	if line := ParseVersionOutput(string(out)); line != "" {
		return line
	}
	if err != nil {
		return err.Error()
	}
	return "ok"
}

func DatabaseDataDir(path string) string {
	if path == "" {
		return "."
	}
	return filepath.Dir(path)
}

package databases

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controldb "github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
)

type dockerMySQLExecutor struct {
	container string
}

func (e dockerMySQLExecutor) ExecSQL(ctx context.Context, statement string) error {
	_, err := e.query(ctx, statement)
	return err
}

func (e dockerMySQLExecutor) QuerySQL(ctx context.Context, statement string) (string, error) {
	return e.query(ctx, statement)
}

func (e dockerMySQLExecutor) Dump(context.Context, string, io.Writer) error {
	return errors.New("dump is not used by database connectivity E2E")
}

func (e dockerMySQLExecutor) Restore(context.Context, io.Reader) error {
	return errors.New("restore is not used by database connectivity E2E")
}

func (e dockerMySQLExecutor) query(ctx context.Context, statement string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", e.container, "mysql", "-uroot", "--batch", "--skip-column-names")
	cmd.Stdin = strings.NewReader(statement)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker mysql execution failed: %s", sanitizeMySQLError(string(out)))
	}
	return string(out), nil
}

func TestDockerDatabaseConnectivityE2E(t *testing.T) {
	if os.Getenv("DEVBOX_DOCKER_E2E") != "1" {
		t.Skip("set DEVBOX_DOCKER_E2E=1 to run Docker database connectivity E2E")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("Docker unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	network := "devbox-e2e-" + suffix
	mysqlContainer := "devbox-e2e-mysql-" + suffix
	appImage := "devbox-e2e-php:" + suffix

	runDocker := func(stdin string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "docker", args...)
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out))
	}
	runDocker("", "network", "create", network)
	t.Cleanup(func() {
		_ = exec.Command("docker", "container", "rm", "-f", mysqlContainer).Run()
		_ = exec.Command("docker", "network", "rm", network).Run()
		_ = exec.Command("docker", "image", "rm", "-f", appImage).Run()
	})
	runDocker("", "run", "-d", "--name", mysqlContainer, "--network", network,
		"-e", "MYSQL_ALLOW_EMPTY_PASSWORD=yes", "mysql:8.4")

	ready := false
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); {
		cmd := exec.CommandContext(ctx, "docker", "exec", mysqlContainer, "mysqladmin", "ping", "--silent", "-uroot")
		if err := cmd.Run(); err == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		t.Fatal("MySQL E2E container did not become ready")
	}

	db, err := controldb.Open(filepath.Join(t.TempDir(), "devbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := controldb.Migrate(ctx, db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	projectDir := t.TempDir()
	_, err = db.ExecContext(ctx, `INSERT INTO projects(
		id,name,slug,description,status,work_dir,created_at,updated_at,source_type,local_path,
		runtime,runtime_version,container_policy,deployment_mode,working_directory,
		build_command,start_command,healthcheck,auto_start,current_commit
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"e2e-project", "E2E PHP", "e2e-php", "", "ready", projectDir, now, now, "local", projectDir,
		"php", "8.3", "auto", "docker", "", "", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	store := &fakeSecretStore{values: map[string][]byte{}}
	provider := &MySQLProvider{
		cfg: MySQLConfig{
			Host: mysqlContainer, Port: 3306, AdminUser: "root", ApplicationHost: "%",
			ApplicationEndpointHost: mysqlContainer, ApplicationEndpointPort: 3306,
		},
		secrets: store,
		exec:    dockerMySQLExecutor{container: mysqlContainer},
	}
	service := &Service{repo: NewRepository(db), engine: provider, secrets: store}
	result, err := service.ProvisionProject(ctx, "e2e-project", "mysql", "utf8mb4", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Credential.Host != mysqlContainer || result.Credential.Port != 3306 {
		t.Fatalf("unexpected application endpoint: %s:%d", result.Credential.Host, result.Credential.Port)
	}

	buildDir := t.TempDir()
	dockerfile := `FROM php:8.3-cli
RUN docker-php-ext-install pdo_mysql
WORKDIR /app
COPY check.php /app/check.php
CMD ["php", "/app/check.php"]
`
	php := `<?php
$dsn = sprintf('mysql:host=%s;port=%s;dbname=%s;charset=utf8mb4', getenv('DB_HOST'), getenv('DB_PORT'), getenv('DB_DATABASE'));
$pdo = new PDO($dsn, getenv('DB_USERNAME'), getenv('DB_PASSWORD'), [PDO::ATTR_ERRMODE => PDO::ERRMODE_EXCEPTION]);
echo $pdo->query('SELECT 1')->fetchColumn();
`
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(dockerfile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "check.php"), []byte(php), 0o600); err != nil {
		t.Fatal(err)
	}
	runDocker("", "build", "-t", appImage, buildDir)

	envPath := filepath.Join(t.TempDir(), "database.env")
	envBody := fmt.Sprintf("DB_HOST=%s\nDB_PORT=%d\nDB_DATABASE=%s\nDB_USERNAME=%s\nDB_PASSWORD=%s\n",
		result.Credential.Host, result.Credential.Port, result.Credential.Database,
		result.Credential.Username, result.Credential.Password)
	if err := os.WriteFile(envPath, []byte(envBody), 0o600); err != nil {
		t.Fatal(err)
	}
	output := runDocker("", "run", "--rm", "--network", network, "--env-file", envPath, appImage)
	if output != "1" {
		t.Fatalf("expected SELECT 1 result, got %q", output)
	}
}

package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func TestEnsureNetworkCreatesOnceAndIsIdempotent(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{
		{err: fmt.Errorf("%w: missing", ErrNotFound)},
		{stdout: "network-id\n"},
		{stdout: `[{"Name":"devbox-apps"}]`},
	}}
	provider := newCLIProviderWithRunner(runner)
	if err := provider.EnsureNetwork(context.Background(), "devbox-apps"); err != nil {
		t.Fatal(err)
	}
	if err := provider.EnsureNetwork(context.Background(), "devbox-apps"); err != nil {
		t.Fatal(err)
	}
	creates := 0
	for _, call := range runner.calls {
		if strings.Join(call, "|") == "network|create|devbox-apps" {
			creates++
		}
	}
	if creates != 1 {
		t.Fatalf("expected one network create, got %d calls: %#v", creates, runner.calls)
	}
}

func TestCommandErrorClassifiesDockerNetworkNotFoundVariant(t *testing.T) {
	err := commandError("docker", "Error response from daemon: network devbox-apps not found", errors.New("exit status 1"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("network not-found variant must map to ErrNotFound, got %v", err)
	}
}

func TestConnectNetworkRecreatesMissingNetworkAndRetries(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{
		{
			stderr: "Error response from daemon: network devbox-apps not found",
			err:    fmt.Errorf("%w: network devbox-apps not found", ErrNotFound),
		},
		{err: fmt.Errorf("%w: missing", ErrNotFound)},
		{stdout: "network-id\n"},
		{},
	}}
	provider := newCLIProviderWithRunner(runner)
	if err := provider.ConnectNetwork(context.Background(), "abc123", "devbox-apps"); err != nil {
		t.Fatal(err)
	}

	var creates, connects int
	for _, call := range runner.calls {
		switch strings.Join(call, "|") {
		case "network|create|devbox-apps":
			creates++
		case "network|connect|devbox-apps|abc123":
			connects++
		}
	}
	if creates != 1 || connects != 2 {
		t.Fatalf("expected one recreate and two connect attempts, got creates=%d connects=%d calls=%#v", creates, connects, runner.calls)
	}
}

func TestContainerSensitiveEnvironmentDoesNotAppearInArguments(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{{stdout: "container-id\n"}}}
	provider := newCLIProviderWithRunner(runner)
	secret := "super-secret-database-password"
	_, err := provider.Create(context.Background(), providers.ContainerSpec{
		Name:                 "devbox-app-test",
		Image:                "php:8.3-cli",
		SensitiveEnvironment: map[string]string{"DB_PASSWORD": secret},
		Networks:             []string{"devbox-apps"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("unexpected docker calls: %#v", runner.calls)
	}
	args := runner.calls[0]
	joined := strings.Join(args, " ")
	if strings.Contains(joined, secret) {
		t.Fatalf("secret leaked into Docker arguments: %s", joined)
	}
	if !strings.Contains(joined, "--network devbox-apps") {
		t.Fatalf("managed container was not attached to devbox-apps: %s", joined)
	}
	envFile := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--env-file" {
			envFile = args[i+1]
			break
		}
	}
	if envFile == "" {
		t.Fatal("expected secret-safe --env-file")
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Fatalf("temporary env file must be removed after create, stat err=%v", err)
	}
}

func TestComposeDatabaseOverrideUsesComposeDNSAndLeavesProjectFileUntouched(t *testing.T) {
	projectDir := t.TempDir()
	source := filepath.Join(projectDir, "docker-compose.yml")
	original := []byte("services:\n  web:\n    image: php:8.3-apache\n  db:\n    image: mysql:8.4\n")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVBOX_COMPOSE_DATABASE_DIR", filepath.Join(t.TempDir(), "overrides"))
	override, err := renderComposeDatabaseOverride("web", map[string]string{
		"DB_HOST":     "db",
		"DB_PORT":     "3306",
		"DB_DATABASE": "plan",
		"DB_USERNAME": "plan_user",
		"DB_PASSWORD": "compose-secret",
	}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(override), `DB_HOST: "db"`) || !strings.Contains(string(override), `DB_PORT: "3306"`) {
		t.Fatalf("Compose override does not use Compose DNS: %s", override)
	}
	path, err := composeDatabaseOverridePath(projectDir, "plan", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeDatabaseOverride(path, override); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("database override must be mode 0600, got %o", info.Mode().Perm())
	}
	current, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(original) {
		t.Fatalf("project-owned Compose file was modified:\n%s", current)
	}
	if strings.Contains(string(current), "compose-secret") {
		t.Fatal("secret was written to project-owned Compose file")
	}
}

func TestRenderComposeOverrideAddsHostGateway(t *testing.T) {
	override, err := renderComposeDatabaseOverride("web", map[string]string{
		"DB_HOST": "host.docker.internal",
		"DB_PORT": "3306",
	}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(override)
	if !strings.Contains(text, "extra_hosts:") ||
		!strings.Contains(text, `"host.docker.internal:host-gateway"`) {
		t.Fatalf("host gateway mapping missing from Compose override:\n%s", text)
	}
}

func TestRenderComposeOverrideHostGatewayWithoutDatabaseEnvironment(t *testing.T) {
	override, err := renderComposeDatabaseOverride("web", map[string]string{}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(override)
	if strings.Contains(text, "environment:") {
		t.Fatalf("host-access-only override must not create an empty database environment block:\n%s", text)
	}
	if !strings.Contains(text, "extra_hosts:") ||
		!strings.Contains(text, `"host.docker.internal:host-gateway"`) {
		t.Fatalf("host gateway mapping missing from access-only Compose override:\n%s", text)
	}
}

func TestRenderManagedComposeOverrideDeclaresExternalDevBoxNetwork(t *testing.T) {
	override, err := renderComposeDatabaseOverride("web", map[string]string{"DB_HOST": "devbox-mysql"}, "devbox-apps", false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(override)
	if !strings.Contains(text, `DB_HOST: "devbox-mysql"`) ||
		!strings.Contains(text, `"devbox-apps":`) ||
		!strings.Contains(text, "external: true") {
		t.Fatalf("unexpected managed database Compose override:\n%s", text)
	}
}

type envAwareStubRunner struct {
	stubRunner
	environment map[string]string
}

func (r *envAwareStubRunner) RunEnv(ctx context.Context, environment map[string]string, args ...string) ([]byte, []byte, error) {
	r.environment = make(map[string]string, len(environment))
	for key, value := range environment {
		r.environment[key] = value
	}
	return r.Run(ctx, args...)
}

func TestDatabaseConnectionToDockerHostAddsHostGateway(t *testing.T) {
	runner := &envAwareStubRunner{stubRunner: stubRunner{responses: []runnerResponse{
		{stdout: `[{"Id":"mysql-image"}]`},
		{stdout: "1\n"},
	}}}
	provider := newCLIProviderWithRunner(runner)
	err := provider.TestDatabaseConnection(context.Background(), "", providers.DatabaseConnection{
		Mode: providers.DatabaseModeExternal, Host: "host.docker.internal", Port: 3306,
		Database: "wordpress", Username: "wordpress",
	}, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	last := strings.Join(runner.calls[len(runner.calls)-1], " ")
	if !strings.Contains(last, "--add-host host.docker.internal:host-gateway") {
		t.Fatalf("host gateway mapping missing from Docker database test: %s", last)
	}
	if !strings.Contains(last, "--host host.docker.internal") {
		t.Fatalf("database test did not use host.docker.internal: %s", last)
	}
}

func TestDatabaseConnectionFromDockerNetworkKeepsPasswordOutOfArguments(t *testing.T) {
	secret := "container-network-secret"
	runner := &envAwareStubRunner{stubRunner: stubRunner{responses: []runnerResponse{
		{stdout: `[{"Name":"devbox-apps"}]`},
		{stdout: `[{"Id":"mysql-image"}]`},
		{stdout: "1\n"},
	}}}
	provider := newCLIProviderWithRunner(runner)
	err := provider.TestDatabaseConnection(context.Background(), "devbox-apps", providers.DatabaseConnection{
		Mode: providers.DatabaseModeManaged, Host: "devbox-mysql", Port: 3306,
		Database: "plan", Username: "plan_user",
	}, []byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if runner.environment["MYSQL_PWD"] != secret {
		t.Fatal("database password was not passed through the Docker client process environment")
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), secret) {
			t.Fatalf("database password leaked into Docker arguments: %#v", call)
		}
	}
	last := strings.Join(runner.calls[len(runner.calls)-1], " ")
	if !strings.Contains(last, "--network devbox-apps") ||
		!strings.Contains(last, "--host devbox-mysql") ||
		!strings.Contains(last, "--execute SELECT 1") {
		t.Fatalf("unexpected application-network connectivity command: %s", last)
	}
}

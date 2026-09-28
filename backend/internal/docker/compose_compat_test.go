package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestComposeArgsAvoidsProjectDirectoryFlag(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	args, err := composeArgs(dir, "example-app")
	if err != nil {
		t.Fatalf("composeArgs() error = %v", err)
	}
	if slices.Contains(args, "--project-directory") {
		t.Fatalf("composeArgs() contains unsupported --project-directory flag: %#v", args)
	}

	want := []string{"compose", "--project-name", "example-app", "--file", composeFile}
	if !slices.Equal(args, want) {
		t.Fatalf("composeArgs() = %#v, want %#v", args, want)
	}
}

func TestComposeValidateFallsBackToLegacyDockerCompose(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(config, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dockerRunner := &stubRunner{responses: []runnerResponse{{
		err: errors.New("docker command failed: docker: 'compose' is not a docker command"),
	}}}
	legacyRunner := &stubRunner{responses: []runnerResponse{
		{stdout: "docker-compose version 1.29.2\n"},
		{},
	}}
	provider := newCLIProviderWithComposeRunners(dockerRunner, legacyRunner)

	if err := provider.ComposeValidate(context.Background(), dir, "sample"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(dockerRunner.calls[0], "|"); got != "compose|version" {
		t.Fatalf("unexpected docker compose probe: %s", got)
	}
	if got := strings.Join(legacyRunner.calls[0], "|"); got != "version" {
		t.Fatalf("unexpected legacy compose probe: %s", got)
	}
	want := "--project-name|sample|--file|" + config + "|config|--quiet"
	if got := strings.Join(legacyRunner.calls[1], "|"); got != want {
		t.Fatalf("unexpected legacy compose validation: got %s want %s", got, want)
	}
}

func TestComposePSFallsBackWhenLegacyFormatJSONIsUnsupported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dockerRunner := &stubRunner{responses: []runnerResponse{
		{err: errors.New("docker command failed: docker: 'compose' is not a docker command")},
		{err: errors.New("docker command failed: docker: 'compose' is not a docker command")},
		{stdout: "[{\"Id\":\"abc123\",\"Name\":\"/sample-app-1\",\"Config\":{\"Image\":\"php:8.3\",\"Labels\":{\"com.docker.compose.service\":\"app\"}},\"State\":{\"Status\":\"running\",\"Health\":{\"Status\":\"healthy\"}}}]"},
	}}
	legacyRunner := &stubRunner{responses: []runnerResponse{
		{stdout: "docker-compose version 1.29.2\n"},
		{err: errors.New("docker-compose command failed: No such option: --format")},
		{stdout: "docker-compose version 1.29.2\n"},
		{stdout: "abc123\n"},
	}}
	provider := newCLIProviderWithComposeRunners(dockerRunner, legacyRunner)

	items, err := provider.ComposePS(context.Background(), dir, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Service != "app" || items[0].State != "running" || items[0].Health != "healthy" {
		t.Fatalf("unexpected compose processes: %#v", items)
	}
}

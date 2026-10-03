package docker

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

func TestManagedExtraHostArgsAddsDockerHostGateway(t *testing.T) {
	args, err := managedExtraHostArgs(containerspec.DeploymentSpec{
		ExtraHosts: map[string]string{"host.docker.internal": "host-gateway"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, "|"); got != "--add-host|host.docker.internal:host-gateway" {
		t.Fatalf("managedExtraHostArgs() = %s", got)
	}
}

func TestManagedExtraHostArgsRejectsArbitraryMapping(t *testing.T) {
	_, err := managedExtraHostArgs(containerspec.DeploymentSpec{
		ExtraHosts: map[string]string{"evil.internal": "1.2.3.4"},
	})
	if err == nil {
		t.Fatal("expected arbitrary host mapping to be rejected")
	}
}

func TestManagedMountArgsAddsLiveBindAndDependencyVolume(t *testing.T) {
	dir := t.TempDir() + " project with spaces"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	args, err := managedMountArgs(containerspec.DeploymentSpec{
		BindMounts:       map[string]string{dir: "/app"},
		AnonymousVolumes: []string{"/app/vendor"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "--mount|type=bind,source=" + dir + ",target=/app|--mount|type=volume,target=/app/vendor"
	if got := strings.Join(args, "|"); got != want {
		t.Fatalf("managedMountArgs() = %s, want %s", got, want)
	}
}

func TestManagedMountArgsRejectsRelativeSource(t *testing.T) {
	_, err := managedMountArgs(containerspec.DeploymentSpec{
		BindMounts: map[string]string{"relative/path": "/app"},
	})
	if err == nil {
		t.Fatal("expected relative bind mount source to be rejected")
	}
}

func TestValidateManagedUserAcceptsNumericUIDAndGID(t *testing.T) {
	if err := validateManagedUser("1000:1000"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"1000", "root:root", "1000:bad", "-1:1000"} {
		if err := validateManagedUser(value); err == nil {
			t.Errorf("validateManagedUser(%q) accepted invalid value", value)
		}
	}
}

func TestBuildManagedPrefersBuildxWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	runner := &stubRunner{responses: []runnerResponse{
		{stdout: "27.0.0"},
		{stdout: "github.com/docker/buildx v0.16.2"},
		{stdout: "built"},
	}}
	provider := newCLIProviderWithRunner(runner)

	err := provider.BuildManaged(context.Background(), containerspec.DeploymentSpec{
		ContextDir: dir,
		Dockerfile: "FROM busybox:latest\nCMD [\"true\"]\n",
		Image:      "devbox/test-buildx:latest",
		Labels:     map[string]string{"io.devbox.managed": "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("expected docker info, buildx probe and build; got %#v", runner.calls)
	}
	got := strings.Join(runner.calls[2], "|")
	for _, expected := range []string{"buildx|build", "--load", "--progress=plain", "--pull", "--tag|devbox/test-buildx:latest"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("buildx invocation is missing %q: %s", expected, got)
		}
	}
	if strings.Contains(got, "image|build") {
		t.Fatalf("legacy image build was used despite buildx availability: %s", got)
	}
}

func TestBuildManagedRetainsCompleteFailedBuildOutput(t *testing.T) {
	output := strings.Repeat("dependency output line\n", 400) + "Composer dependency resolution failed"
	runner := &stubRunner{responses: []runnerResponse{
		{},
		{err: errors.New("buildx plugin unavailable")},
		{stderr: output, err: errors.New("exit status 2")},
	}}
	provider := newCLIProviderWithRunner(runner)
	err := provider.BuildManaged(context.Background(), containerspec.DeploymentSpec{
		ContextDir: t.TempDir(), Dockerfile: "FROM busybox:latest\n", Image: "devbox/test-output:latest",
	})
	var buildErr *BuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("BuildManaged() error = %v, want BuildError", err)
	}
	if buildErr.BuildLogOutput() != output {
		t.Fatalf("failed build log was truncated: got %d bytes, want %d", len(buildErr.BuildLogOutput()), len(output))
	}
	if !strings.Contains(buildErr.Error(), "exit status 2") {
		t.Fatalf("BuildError summary lost command failure: %v", buildErr)
	}
}

func TestCompactCommandOutputKeepsFinalDockerError(t *testing.T) {
	input := strings.Repeat("legacy builder warning ", 100) + "\ncomposer dependency resolution failed: ext-ldap is missing"
	got := compactCommandOutput(input, 300)
	if !strings.Contains(got, "docker output truncated") {
		t.Fatalf("expected truncation marker: %q", got)
	}
	if !strings.Contains(got, "composer dependency resolution failed: ext-ldap is missing") {
		t.Fatalf("final actionable error was lost: %q", got)
	}
}

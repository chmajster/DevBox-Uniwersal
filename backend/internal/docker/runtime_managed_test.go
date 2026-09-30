package docker

import (
	"context"
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
	dir := t.TempDir()
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

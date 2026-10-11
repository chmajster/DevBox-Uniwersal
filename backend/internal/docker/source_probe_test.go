package docker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

func TestSourceProbeRejectsSuccessfulDaemonWithoutHostWrite(t *testing.T) {
	root := t.TempDir()
	runner := &stubRunner{responses: []runnerResponse{{}, {}}}
	provider := &CLIProvider{runner: runner}
	err := provider.VerifyManagedSource(context.Background(), containerspec.DeploymentSpec{Runtime: "go", Image: "golang:1.26-bookworm", BindMounts: map[string]string{root: "/app"}})
	if err == nil || !strings.Contains(err.Error(), "not visible") {
		t.Fatalf("unverified daemon mapping accepted: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(root, ".devbox-mount-probe-*"))
	if len(files) != 0 {
		t.Fatalf("probe artifacts retained: %v", files)
	}
	if len(runner.calls) < 2 {
		t.Fatal("probe container cleanup was not attempted")
	}
}

func TestSourceProbeRejectsServiceReadOnlySource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses local filesystem permission checks")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o700)
	provider := &CLIProvider{runner: &stubRunner{}}
	err := provider.VerifyManagedSource(context.Background(), containerspec.DeploymentSpec{Runtime: "python", BindMounts: map[string]string{root: "/app"}})
	if err == nil || !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("read-only source accepted: %v", err)
	}
}

func TestCustomSourceProbeUsesHelperAndRejectsWrongMapping(t *testing.T) {
	root := t.TempDir()
	runner := &stubRunner{responses: []runnerResponse{{stdout: `[{}]`}, {}, {}}}
	provider := &CLIProvider{runner: runner}
	err := provider.VerifyManagedSource(context.Background(), containerspec.DeploymentSpec{Runtime: "custom", Image: "scratch-app", BindMounts: map[string]string{root: "/srv"}})
	if err == nil || !strings.Contains(err.Error(), "not visible") {
		t.Fatalf("custom daemon mapping accepted: %v", err)
	}
	if !strings.Contains(strings.Join(runner.calls[1], " "), "alpine:3.22") || strings.Contains(strings.Join(runner.calls[1], " "), "scratch-app") {
		t.Fatalf("custom image was used as a shell probe: %v", runner.calls)
	}
	files, _ := filepath.Glob(filepath.Join(root, ".devbox-mount-probe-*"))
	if len(files) != 0 {
		t.Fatalf("probe artifacts retained: %v", files)
	}
}

func TestOCISourceProbeDoesNotPullOrMount(t *testing.T) {
	runner := &stubRunner{}
	provider := &CLIProvider{runner: runner}
	if err := provider.VerifyManagedSource(context.Background(), containerspec.DeploymentSpec{Runtime: "custom", Image: "nginx:alpine"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("OCI application attempted a source probe: %v", runner.calls)
	}
}

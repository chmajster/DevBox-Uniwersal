package docker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

// Run as the non-root DevBox service account against a Windows-backed WSL
// directory without metadata. Its files are writable even though chmod fails.
func TestSourceProbeWSLChmodDeniedDockerIntegration(t *testing.T) {
	base := os.Getenv("DEVBOX_TEST_WSL_ROOT")
	if os.Getenv("DEVBOX_TEST_DOCKER_PORTS") != "1" || base == "" || os.Geteuid() == 0 {
		t.Skip("requires Docker, DEVBOX_TEST_WSL_ROOT and a non-root service account")
	}
	root, err := os.MkdirTemp(base, "devbox-wsl-permissions-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	file, err := os.CreateTemp(root, "chmod-check-*")
	if err != nil {
		t.Fatal(err)
	}
	chmodErr := file.Chmod(0o644)
	_ = file.Close()
	_ = os.Remove(file.Name())
	if chmodErr == nil {
		t.Skip("filesystem supports chmod; this regression requires DrvFS without metadata")
	}
	if !errors.Is(chmodErr, syscall.EPERM) {
		t.Fatalf("unexpected filesystem error: %v", chmodErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	provider := NewCLIProvider()
	if err := provider.EnsureImage(ctx, "alpine:3.22"); err != nil {
		t.Fatal(err)
	}
	spec := containerspec.DeploymentSpec{Runtime: "php", Image: "alpine:3.22", BindMounts: map[string]string{root: "/app"}}
	if err := provider.VerifyManagedSource(ctx, spec); err != nil {
		t.Fatalf("writable DrvFS source was rejected: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, ".devbox-mount-probe-*"))
	if err != nil || len(files) != 0 {
		t.Fatalf("probe artifacts retained: %v, %v", files, err)
	}
}

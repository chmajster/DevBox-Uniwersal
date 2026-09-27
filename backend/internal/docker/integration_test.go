package docker

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestDockerIntegrationStatusWhenAvailable(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker CLI is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := NewCLIProvider()
	if err := provider.Available(ctx); err != nil {
		if errors.Is(err, ErrUnavailable) {
			t.Skip("docker daemon is unavailable")
		}
		t.Skipf("docker cannot be queried in this environment: %v", err)
	}
	status, err := provider.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.ServerVersion == "" {
		t.Fatalf("unexpected docker status: %#v", status)
	}
}

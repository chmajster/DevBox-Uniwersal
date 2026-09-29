package docker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectComposeProjectNetworkAttachesAllRunningContainers(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services:\n  web:\n    image: nginx:alpine\n  worker:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &stubRunner{responses: []runnerResponse{
		{stdout: "[{\"Name\":\"devbox-apps\"}]"},
		{stdout: "Docker Compose version v2.29.0\n"},
		{stdout: "abc123\ndef456\n"},
		{},
		{},
	}}
	provider := newCLIProviderWithRunner(runner)
	if err := provider.ConnectComposeProjectNetwork(context.Background(), dir, "sample", "devbox-apps"); err != nil {
		t.Fatal(err)
	}

	joined := make([]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		joined = append(joined, strings.Join(call, "|"))
	}
	wantConnectA := "network|connect|devbox-apps|abc123"
	wantConnectB := "network|connect|devbox-apps|def456"
	if !containsString(joined, wantConnectA) || !containsString(joined, wantConnectB) {
		t.Fatalf("Compose containers were not attached to shared network: %#v", joined)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

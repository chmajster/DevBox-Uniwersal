package docker

import (
	"context"
	"strings"
	"testing"
)

func TestVersionUsesDockerVersionCommand(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{{stdout: "{\"Client\":{\"Version\":\"27.1.0\"},\"Server\":{\"Version\":\"27.1.0\"}}\\n"}}}
	provider := newCLIProviderWithRunner(runner)

	version, err := provider.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version.Client != "27.1.0" || version.Server != "27.1.0" {
		t.Fatalf("unexpected docker version: %#v", version)
	}
	got := strings.Join(runner.calls[0], "|")
	if got != "version|--format|{{json .}}" {
		t.Fatalf("unexpected docker version arguments: %s", got)
	}
}

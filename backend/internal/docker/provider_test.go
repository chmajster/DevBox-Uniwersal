package docker

import (
	"context"
	"io"
	"strings"
	"testing"
)

type runnerResponse struct {
	stdout string
	stderr string
	err    error
}

type stubRunner struct {
	responses []runnerResponse
	calls     [][]string
}

func (r *stubRunner) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	response := r.responses[0]
	r.responses = r.responses[1:]
	return []byte(response.stdout), []byte(response.stderr), response.err
}

func (r *stubRunner) Stream(_ context.Context, args ...string) (io.ReadCloser, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	response := r.responses[0]
	r.responses = r.responses[1:]
	return io.NopCloser(strings.NewReader(response.stdout)), response.err
}

func TestListContainersParsesDockerJSONLines(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{{stdout: "{\"ID\":\"abc123\",\"Names\":\"web\",\"Image\":\"nginx:latest\",\"State\":\"running\",\"Status\":\"Up 1 minute\",\"Ports\":\"0.0.0.0:8080->80/tcp\",\"CreatedAt\":\"today\",\"Labels\":\"com.docker.compose.project=plan,com.docker.compose.service=web,io.devbox.project=project-123\"}\n"}}}
	provider := newCLIProviderWithRunner(runner)
	items, err := provider.ListContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "web" || items[0].State != "running" {
		t.Fatalf("unexpected containers: %#v", items)
	}
	if items[0].ComposeProject != "plan" || items[0].ProjectID != "project-123" {
		t.Fatalf("container project metadata was not parsed: %#v", items[0])
	}
	if got := strings.Join(runner.calls[0], " "); strings.Contains(got, "sh -c") || strings.Contains(got, "bash -c") {
		t.Fatalf("provider must not invoke a shell: %s", got)
	}
}

func TestDockerListLabelReturnsRequestedMetadata(t *testing.T) {
	labels := "com.docker.compose.project=plan,io.devbox.project=project-123,other=value"
	if got := dockerListLabel(labels, "com.docker.compose.project"); got != "plan" {
		t.Fatalf("compose project = %q, want plan", got)
	}
	if got := dockerListLabel(labels, "io.devbox.project"); got != "project-123" {
		t.Fatalf("DevBox project = %q, want project-123", got)
	}
	if got := dockerListLabel(labels, "missing"); got != "" {
		t.Fatalf("missing label = %q, want empty", got)
	}
}

func TestExecUsesAllowlistAndFixedArguments(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{{stdout: "uid=0(root)\n"}}}
	provider := newCLIProviderWithRunner(runner)
	out, err := provider.Exec(context.Background(), "container-1", ExecIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if out != "uid=0(root)\n" {
		t.Fatalf("unexpected output: %q", out)
	}
	got := strings.Join(runner.calls[0], "|")
	if got != "container|exec|container-1|id" {
		t.Fatalf("unexpected docker arguments: %s", got)
	}
	if _, err := provider.Exec(context.Background(), "container-1", ExecCommand("rm -rf /")); err == nil {
		t.Fatal("expected arbitrary exec command to be rejected")
	}
}

func TestLogsClampTailAndDoNotAcceptArguments(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{{stdout: "log"}}}
	provider := newCLIProviderWithRunner(runner)
	stream, err := provider.Logs(context.Background(), "container-1", 999999, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got := strings.Join(runner.calls[0], "|")
	if got != "container|logs|--tail|5000|container-1" {
		t.Fatalf("unexpected docker arguments: %s", got)
	}
}

func TestMergeProcessEnvironmentReplacesExistingSecret(t *testing.T) {
	got := mergeProcessEnvironment(
		[]string{"PATH=/usr/bin", "MYSQL_PWD=host-value", "OTHER=keep"},
		map[string]string{"MYSQL_PWD": "binding-secret", "DB_HOST": "devbox-mysql"},
	)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "MYSQL_PWD=host-value") {
		t.Fatalf("inherited secret was not replaced: %s", joined)
	}
	if !strings.Contains(joined, "MYSQL_PWD=binding-secret") ||
		!strings.Contains(joined, "DB_HOST=devbox-mysql") ||
		!strings.Contains(joined, "OTHER=keep") {
		t.Fatalf("unexpected merged process environment: %s", joined)
	}
}

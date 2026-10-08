package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

func TestHostingRuntimeFilesStayOutsideSourceAndExcludeSecrets(t *testing.T) {
	source := t.TempDir()
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "index.php"), []byte("<?php"), 0644); err != nil {
		t.Fatal(err)
	}
	spec, err := containerspec.GenerateManagedProfile("hosting-app", source, "php", "8.4", nil, "", 32781, "")
	if err != nil {
		t.Fatal(err)
	}
	spec.SensitiveEnvironment = map[string]string{"PASSWORD": "must-never-persist"}
	spec.Networks = []string{"devbox-hosting-app"}
	spec.Command = []string{"php", "-S", "0.0.0.0:8080"}
	provider := NewCLIProvider().WithRuntimeRoot(data)
	if err := provider.SaveManagedRuntime(spec); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Dockerfile", "compose.yaml", "metadata.json"} {
		path := filepath.Join(data, "hosting-app", "runtime", name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "must-never-persist") {
			t.Fatal("secret persisted")
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("unsafe generated file permissions", err)
		}
	}
	files, _ := os.ReadDir(source)
	if len(files) != 1 {
		t.Fatal("generated files polluted source")
	}
	if err := provider.RemoveRuntimeFiles("../source"); err == nil {
		t.Fatal("accepted traversal")
	}
	if err := provider.RemoveRuntimeFiles("hosting-app"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(source, "index.php")); err != nil {
		t.Fatal("source deleted")
	}
}

func TestHostingExitedContainerHasImmediateErrorAndBothLogStreams(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{{stdout: `{"Running":true,"Status":"restarting","ExitCode":127,"Error":"executable missing"}`}, {stdout: "startup output", stderr: "missing program"}}}
	provider := newCLIProviderWithRunner(runner)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := provider.waitManagedHealthy(ctx, "devbox-hosting-app", 32781)
	for _, expected := range []string{"exit=127", "startup output", "missing program"} {
		if err == nil || !strings.Contains(err.Error(), expected) {
			t.Fatalf("error lacks %s: %v", expected, err)
		}
	}
}

func TestHostingLogsPreserveStdoutAndStderr(t *testing.T) {
	runner := &stubRunner{responses: []runnerResponse{{stdout: "2026-10-04T10:00:00Z stdout line\n", stderr: "2026-10-04T10:00:01Z stderr line\n"}}}
	lines, err := newCLIProviderWithRunner(runner).LogStreams(context.Background(), "devbox-hosting-app", 200)
	if err != nil || len(lines) != 2 || lines[0].Stream != "stdout" || lines[1].Stream != "stderr" {
		t.Fatalf("logs %+v %v", lines, err)
	}
}

type hostingComposeRunner struct {
	directory string
	payload   map[string]any
	args      []string
}

func (r *hostingComposeRunner) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	text := strings.Join(args, " ")
	if text == "compose version" {
		return []byte("Docker Compose version v2.40.3"), nil, nil
	}
	if strings.Contains(text, "config --format json") {
		body, _ := json.Marshal(map[string]any{"services": map[string]any{"web": map[string]any{"volumes": []any{map[string]any{"type": "bind", "source": r.directory, "target": "/app", "read_only": true}}}}, "networks": map[string]any{"default": map[string]any{}, "external": map[string]any{"external": true}}, "volumes": map[string]any{"data": map[string]any{}}})
		return body, nil, nil
	}
	if strings.Contains(text, " up ") {
		r.args = append([]string(nil), args...)
		for i := range args {
			if args[i] == "-f" && i+1 < len(args) && strings.HasPrefix(filepath.Base(args[i+1]), "devbox-compose-labels-") {
				body, err := os.ReadFile(args[i+1])
				if err != nil {
					return nil, nil, err
				}
				if err := json.Unmarshal(body, &r.payload); err != nil {
					return nil, nil, err
				}
			}
		}
		return nil, nil, nil
	}
	return nil, nil, errors.New("unexpected command " + text)
}
func (r *hostingComposeRunner) Stream(context.Context, ...string) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func TestHostingComposeOverridePreservesSourceAndLabelsResources(t *testing.T) {
	t.Setenv("DEVBOX_COMPOSE_PORTS_DIR", t.TempDir())
	source := t.TempDir()
	content := "services:\n  web:\n    image: node:22\n"
	path := filepath.Join(source, "compose.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	runner := &hostingComposeRunner{directory: source}
	provider := newCLIProviderWithRunner(runner)
	labels := map[string]map[string]string{"web": {"devbox.managed": "true", "devbox.project_id": "abc-123", "devbox.project_name": "App"}}
	env := map[string]map[string]string{"web": {"APP_VALUE": "literal-$VALUE"}}
	err := provider.ComposeUpConfiguredApplication(context.Background(), source, "devbox-abc-123", labels, env, map[string][]string{"web": {"node", "server with spaces.js"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	services := runner.payload["services"].(map[string]any)
	web := services["web"].(map[string]any)
	if web["container_name"] != "devbox-abc-123-web" || web["command"].([]any)[1] != "server with spaces.js" {
		t.Fatal("missing deterministic name/argv")
	}
	mounts := web["volumes"].([]any)
	if mounts[0].(map[string]any)["read_only"] != false {
		t.Fatal("source bind is readonly")
	}
	if web["environment"].(map[string]any)["APP_VALUE"] != "literal-$$VALUE" {
		t.Fatal("environment may be interpolated")
	}
	networks := runner.payload["networks"].(map[string]any)
	if _, ok := networks["external"]; ok {
		t.Fatal("external network modified")
	}
	if networks["default"].(map[string]any)["labels"] == nil || runner.payload["volumes"].(map[string]any)["data"].(map[string]any)["labels"] == nil {
		t.Fatal("resources lack ownership")
	}
	if !strings.Contains(strings.Join(runner.args, " "), "--force-recreate") {
		t.Fatal("recreate omitted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != content {
		t.Fatal("user Compose changed")
	}
}

func TestHostingJobOutputNeverForwardsIntrospection(t *testing.T) {
	for _, args := range [][]string{{"container", "inspect", "build"}, {"compose", "-p", "build", "config", "--format", "json"}, {"image", "inspect", "up"}} {
		if lifecycleOutputAllowed(args) {
			t.Fatalf("inspection forwarded: %v", args)
		}
	}
	for _, args := range [][]string{{"build", "-t", "image", "."}, {"buildx", "build", "."}, {"compose", "-p", "app", "-f", "/source/compose.yaml", "up", "-d"}} {
		if !lifecycleOutputAllowed(args) {
			t.Fatalf("build output suppressed: %v", args)
		}
	}
}

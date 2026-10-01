package docker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
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

	want := []string{"compose", "-p", "example-app", "-f", composeFile}
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
	want := "-p|sample|-f|" + config + "|config|--quiet"
	if got := strings.Join(legacyRunner.calls[1], "|"); got != want {
		t.Fatalf("unexpected legacy compose validation: got %s want %s", got, want)
	}
}

func TestComposeValidateRetriesLegacyWhenPluginRejectsComposeFlags(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(config, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	dockerRunner := &stubRunner{responses: []runnerResponse{
		{stdout: "Docker Compose version v2.29.0\n"},
		{err: errors.New("docker command failed: unknown flag: -p Usage: docker [OPTIONS] COMMAND [ARG...]")},
	}}
	legacyRunner := &stubRunner{responses: []runnerResponse{
		{stdout: "docker-compose version 1.29.2\n"},
		{},
	}}
	provider := newCLIProviderWithComposeRunners(dockerRunner, legacyRunner)

	if err := provider.ComposeValidate(context.Background(), dir, "sample"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(legacyRunner.calls[1], "|"); got != "-p|sample|-f|"+config+"|config|--quiet" {
		t.Fatalf("unexpected legacy fallback invocation: %s", got)
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

func TestComposeTargetPortDetectsPlanStyleWebBinding(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  web:\n    image: php:8.2-apache\n    ports:\n      - \"8080:80\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &stubRunner{responses: []runnerResponse{
		{stdout: "Docker Compose version v2.29.0\n"},
		{stdout: "abc123\n"},
		{stdout: "[{\"Config\":{\"Labels\":{\"com.docker.compose.service\":\"web\"}},\"NetworkSettings\":{\"Ports\":{\"80/tcp\":[{\"HostIp\":\"0.0.0.0\",\"HostPort\":\"8080\"},{\"HostIp\":\"::\",\"HostPort\":\"8080\"}]}}}]"},
	}}
	provider := newCLIProviderWithRunner(runner)

	port, err := provider.ComposeTargetPort(context.Background(), dir, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if port != 8080 {
		t.Fatalf("ComposeTargetPort() = %d, want 8080", port)
	}
}

func TestComposeTargetPortPrefersWebOverPublishedDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  web:\n    image: nginx:alpine\n  db:\n    image: mysql:8\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &stubRunner{responses: []runnerResponse{
		{stdout: "Docker Compose version v2.29.0\n"},
		{stdout: "web123\ndb123\n"},
		{stdout: "[{\"Config\":{\"Labels\":{\"com.docker.compose.service\":\"web\"}},\"NetworkSettings\":{\"Ports\":{\"80/tcp\":[{\"HostIp\":\"0.0.0.0\",\"HostPort\":\"8080\"}]}}}]"},
		{stdout: "[{\"Config\":{\"Labels\":{\"com.docker.compose.service\":\"db\"}},\"NetworkSettings\":{\"Ports\":{\"3306/tcp\":[{\"HostIp\":\"127.0.0.1\",\"HostPort\":\"3306\"}]}}}]"},
	}}
	provider := newCLIProviderWithRunner(runner)

	port, err := provider.ComposeTargetPort(context.Background(), dir, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if port != 8080 {
		t.Fatalf("ComposeTargetPort() = %d, want web host port 8080", port)
	}
}

func TestComposeTargetPortPrefersTypicalApplicationPort(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  worker1:\n    image: busybox\n  worker2:\n    image: busybox\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := &stubRunner{responses: []runnerResponse{
		{stdout: "Docker Compose version v2.29.0\n"},
		{stdout: "one123\ntwo123\n"},
		{stdout: "[{\"Config\":{\"Labels\":{\"com.docker.compose.service\":\"worker1\"}},\"NetworkSettings\":{\"Ports\":{\"9000/tcp\":[{\"HostIp\":\"0.0.0.0\",\"HostPort\":\"19000\"}]}}}]"},
		{stdout: "[{\"Config\":{\"Labels\":{\"com.docker.compose.service\":\"worker2\"}},\"NetworkSettings\":{\"Ports\":{\"9001/tcp\":[{\"HostIp\":\"0.0.0.0\",\"HostPort\":\"19001\"}]}}}]"},
	}}
	provider := newCLIProviderWithRunner(runner)

	port, err := provider.ComposeTargetPort(context.Background(), dir, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if port != 19000 {
		t.Fatalf("ComposeTargetPort() = %d, want typical application port 19000", port)
	}
}

func TestSelectComposePortBindingPrefersWebHTTPPort(t *testing.T) {
	data := []byte(`{
		"services": {
			"db": {"ports": [{"target":3306,"published":"3306","protocol":"tcp"}]},
			"web": {"ports": [{"target":80,"published":"8080","protocol":"tcp"}]}
		}
	}`)
	binding, err := selectComposePortBinding(data)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Service != "web" || binding.RequestedHostPort != 8080 || binding.ContainerPort != 80 {
		t.Fatalf("unexpected binding: %+v", binding)
	}
}

func TestRewriteComposePublishedPortKeepsContainerPortAndChangesHostPort(t *testing.T) {
	data := []byte(`{
		"services": {
			"web": {
				"ports": [{"target":80,"published":"8080","protocol":"tcp"}],
				"volumes": [{"type":"bind","source":"/srv/app","target":"/var/www/html"}]
			},
			"db": {"ports": [{"target":3306,"published":"3306","protocol":"tcp"}]}
		}
	}`)
	rewritten, err := rewriteComposePublishedPort(data, providers.ComposePortBinding{
		Service: "web", RequestedHostPort: 8080, HostPort: 8081, ContainerPort: 80, Protocol: "tcp",
	})
	if err != nil {
		t.Fatal(err)
	}

	var root map[string]any
	if err := json.Unmarshal(rewritten, &root); err != nil {
		t.Fatal(err)
	}
	services := root["services"].(map[string]any)
	web := services["web"].(map[string]any)
	webPorts := web["ports"].([]any)
	webPort := webPorts[0].(map[string]any)
	if got := composeAnyPort(webPort["published"]); got != 8081 {
		t.Fatalf("rewritten web host port = %d, want 8081", got)
	}
	if got := composeAnyPort(webPort["target"]); got != 80 {
		t.Fatalf("container port changed to %d, want 80", got)
	}
	db := services["db"].(map[string]any)
	dbPorts := db["ports"].([]any)
	if got := composeAnyPort(dbPorts[0].(map[string]any)["published"]); got != 3306 {
		t.Fatalf("database port was unexpectedly changed to %d", got)
	}
}

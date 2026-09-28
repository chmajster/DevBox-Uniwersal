package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

// Opt-in: requires a local Docker daemon and permission to pull the generated
// runtime's base image. It exercises real publication and live bind mounts.
func TestPublishedPortsDockerIntegration(t *testing.T) {
	if os.Getenv("DEVBOX_TEST_DOCKER_PORTS") != "1" {
		t.Skip("set DEVBOX_TEST_DOCKER_PORTS=1 to run real Docker publishing checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	provider := NewCLIProvider()
	if err := provider.Available(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVBOX_COMPOSE_PORTS_DIR", t.TempDir())
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dir, "index.html")
	if err := os.WriteFile(index, []byte("devbox-port-before"), 0o644); err != nil {
		t.Fatal(err)
	}
	host, alias, composeHost := freePublishingPorts(t)
	id := fmt.Sprintf("ports-ci-%x", time.Now().UnixNano())
	spec, err := containerspec.GenerateManaged(id, dir, "static", "", nil, "", host)
	if err != nil {
		t.Fatal(err)
	}
	spec, err = containerspec.WithContainerPort(spec, 9090)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		_ = provider.RemoveManaged(cleanup, spec.ContainerName)
		_, _, _ = provider.runner.Run(cleanup, "image", "rm", "-f", spec.Image)
	})
	if err := provider.BuildManaged(ctx, spec); err != nil {
		t.Fatal(err)
	}
	// The second mapping is a generic TCP alias in this fixture. This checks
	// publication, not TLS provisioning, which remains the application's job.
	if err := provider.ReplaceManagedPorts(ctx, spec, []providers.PublishedPort{{HostPort: alias, ContainerPort: 9090}}); err != nil {
		t.Fatal(err)
	}
	read := func(port int) string {
		t.Helper()
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	if read(host) != "devbox-port-before" || read(alias) != "devbox-port-before" {
		t.Fatal("host-to-container publication failed")
	}
	if err := os.WriteFile(index, []byte("devbox-port-after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if read(host) != "devbox-port-after" {
		t.Fatal("port configuration broke live source mounting")
	}

	composeDir := t.TempDir()
	projectName := id + "-compose"
	original := fmt.Sprintf("services:\n  web:\n    image: %s\n    ports:\n      - '80:9090'\n", spec.Image)
	if err := os.WriteFile(filepath.Join(composeDir, "compose.yaml"), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		_ = provider.ComposeDown(cleanup, composeDir, projectName)
	})
	target, err := provider.InspectComposePortTarget(ctx, composeDir, projectName, "")
	if err != nil || target.Service != "web" || target.ContainerPort != 9090 {
		t.Fatalf("real Compose target = %+v, %v", target, err)
	}
	_, err = provider.ConfigureComposePorts(ctx, composeDir, projectName, target.Service, []providers.PublishedPort{{HostPort: composeHost, ContainerPort: 9090}})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.ComposeUp(ctx, composeDir, projectName, ""); err != nil {
		t.Fatal(err)
	}
	if err := provider.CheckPublishedHTTP(ctx, composeHost); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(composeHost), "devbox-port-") {
		t.Fatal("Compose remapping failed")
	}
	unchanged, err := os.ReadFile(filepath.Join(composeDir, "compose.yaml"))
	if err != nil || string(unchanged) != original {
		t.Fatalf("Compose source was modified: %v", err)
	}
}

func freePublishingPorts(t *testing.T) (int, int, int) {
	t.Helper()
	listeners := make([]net.Listener, 0, 3)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	ports := make([]int, 0, 3)
	for i := 0; i < 3; i++ {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners = append(listeners, listener)
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	return ports[0], ports[1], ports[2]
}

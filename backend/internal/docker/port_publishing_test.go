package docker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func TestManagedPublishArgsSupportsIndependentHTTPAndHTTPSMappings(t *testing.T) {
	args, err := managedPublishArgs(containerspec.DeploymentSpec{HostPort: 8081, ContainerPort: 80}, []providers.PublishedPort{{HostPort: 8444, ContainerPort: 443}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--publish", "8081:80", "--publish", "8444:443"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("publish args = %v", args)
	}
	for _, extra := range []providers.PublishedPort{{HostPort: 8081, ContainerPort: 443}, {HostPort: 65536, ContainerPort: 443}, {HostPort: 8443, ContainerPort: 0}} {
		if _, err := managedPublishArgs(containerspec.DeploymentSpec{HostPort: 8081, ContainerPort: 80}, []providers.PublishedPort{extra}); err == nil {
			t.Fatalf("accepted invalid binding %+v", extra)
		}
	}
}

func TestComposePortTargetSelectionAndAmbiguity(t *testing.T) {
	config := []byte(`{"services":{"web":{"ports":[{"target":80,"published":"80"},{"target":443,"published":"443"}]},"db":{"ports":[{"target":5432,"published":"5432"}]}}}`)
	target, err := selectComposePortTarget(config, "")
	if err != nil || target.Service != "web" || target.ContainerPort != 80 {
		t.Fatalf("target = %+v, %v", target, err)
	}
	ambiguous := []byte(`{"services":{"first":{"ports":[{"target":8080}]},"second":{"ports":[{"target":3000}]}}}`)
	if _, err := selectComposePortTarget(ambiguous, ""); err == nil {
		t.Fatal("ambiguous service selected silently")
	}
	target, err = selectComposePortTarget(ambiguous, "second")
	if err != nil || target.ContainerPort != 3000 {
		t.Fatalf("explicit service = %+v, %v", target, err)
	}
	if _, err := selectComposePortTarget(config, "missing"); err == nil {
		t.Fatal("missing service accepted")
	}
	if _, err := selectComposePortTarget([]byte(`{"services":{"web":{"network_mode":"host"}}}`), "web"); err == nil {
		t.Fatal("host-network port publishing accepted")
	}
	unknown, err := selectComposePortTarget([]byte(`{"services":{"web":{"image":"custom"}}}`), "")
	if err != nil || unknown.ContainerPort != 0 {
		t.Fatalf("unknown listener should require explicit configuration: %+v, %v", unknown, err)
	}
}

type portComposeRunner struct {
	failConfig bool
	calls      [][]string
}

func (r *portComposeRunner) Run(_ context.Context, args ...string) ([]byte, []byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if r.failConfig && len(args) > 0 && args[len(args)-1] == "--quiet" {
		return nil, nil, errors.New("invalid compose configuration")
	}
	return nil, nil, nil
}
func (r *portComposeRunner) Stream(context.Context, ...string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestComposePortOverrideIsOutsideSourceAndRollbackRestoresPrevious(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("DEVBOX_COMPOSE_PORTS_DIR", stateRoot)
	source := t.TempDir()
	original := "services:\n  web:\n    image: nginx:alpine\n    ports:\n      - '80:80'\n"
	if err := os.WriteFile(filepath.Join(source, "compose.yaml"), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &portComposeRunner{}
	provider := newCLIProviderWithRunner(runner)
	rollback, err := provider.ConfigureComposePorts(context.Background(), source, "ports-test", "web", []providers.PublishedPort{{HostPort: 8080, ContainerPort: 80}, {HostPort: 8443, ContainerPort: 443}})
	if err != nil {
		t.Fatal(err)
	}
	path, err := composePortOverridePath(source, "ports-test", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, stateRoot+string(os.PathSeparator)) {
		t.Fatalf("state written outside configured root: %s", path)
	}
	first, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(first), "ports: !override") {
		t.Fatalf("override = %s, %v", first, err)
	}
	if strings.Contains(string(first), "image:") || strings.Contains(string(first), "environment:") {
		t.Fatal("override copied unrelated or potentially secret configuration")
	}
	args, err := composeArgs(source, "ports-test")
	if err != nil || len(args) != 7 || args[6] != path {
		t.Fatalf("override missing from lifecycle commands: %v, %v", args, err)
	}
	runner.failConfig = true
	if _, err := provider.ConfigureComposePorts(context.Background(), source, "ports-test", "web", []providers.PublishedPort{{HostPort: 9080, ContainerPort: 80}}); err == nil {
		t.Fatal("invalid override accepted")
	}
	restored, err := os.ReadFile(path)
	if err != nil || string(restored) != string(first) {
		t.Fatalf("previous override not restored: %s, %v", restored, err)
	}
	if err := rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new override not removed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(source, "compose.yaml"))
	if err != nil || string(data) != original {
		t.Fatalf("source Compose changed: %v", err)
	}
	entries, err := os.ReadDir(source)
	if err != nil || len(entries) != 1 {
		t.Fatalf("generated state dirtied application source: %v, %v", entries, err)
	}
}

func TestPortOverrideRejectsSymlinksAndUnmanagedFiles(t *testing.T) {
	t.Setenv("DEVBOX_COMPOSE_PORTS_DIR", t.TempDir())
	source := t.TempDir()
	path, err := composePortOverridePath(source, "safe", true)
	if err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external")
	if err := os.WriteFile(external, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := readPortOverride(path); err == nil {
		t.Fatal("followed an override symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPortOverride(path); err == nil {
		t.Fatal("unmanaged file accepted")
	}
}

func TestPublishedHealthcheckDoesNotFollowExternalRedirect(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1); w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer local.Close()
	port := local.Listener.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := NewCLIProvider().CheckPublishedHTTP(ctx, port); err != nil {
		t.Fatal(err)
	}
	if redirected.Load() != 0 {
		t.Fatal("readiness check followed a redirect outside its local endpoint")
	}
}

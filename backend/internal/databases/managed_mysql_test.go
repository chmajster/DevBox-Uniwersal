package databases

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type managedDockerFake struct {
	calls []string
	spec  providers.ContainerSpec
	state string
}

func (f *managedDockerFake) record(call string)              { f.calls = append(f.calls, call) }
func (f *managedDockerFake) Available(context.Context) error { f.record("available"); return nil }
func (f *managedDockerFake) PullImage(_ context.Context, image string) error {
	f.record("pull-image:" + image)
	return nil
}
func (f *managedDockerFake) Create(_ context.Context, spec providers.ContainerSpec) (providers.ContainerInfo, error) {
	f.record("create:" + spec.Name)
	f.spec = spec
	return providers.ContainerInfo{ID: "mysql-id", Name: spec.Name, State: "created"}, nil
}
func (f *managedDockerFake) Start(_ context.Context, id string) error {
	f.record("start:" + id)
	f.state = "running"
	return nil
}
func (f *managedDockerFake) Stop(_ context.Context, id string) error {
	f.record("stop:" + id)
	f.state = "exited"
	return nil
}
func (f *managedDockerFake) Restart(_ context.Context, id string) error {
	f.record("restart:" + id)
	f.state = "running"
	return nil
}
func (f *managedDockerFake) Remove(_ context.Context, id string) error {
	f.record("remove:" + id)
	return nil
}
func (f *managedDockerFake) Inspect(_ context.Context, id string) (providers.ContainerInfo, error) {
	f.record("inspect:" + id)
	return providers.ContainerInfo{ID: "mysql-id", Name: id, State: f.state}, nil
}
func (f *managedDockerFake) Logs(context.Context, string, int, bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f *managedDockerFake) EnsureNetwork(_ context.Context, name string) error {
	f.record("ensure-network:" + name)
	return nil
}
func (f *managedDockerFake) EnsureVolume(_ context.Context, name string) error {
	f.record("ensure-volume:" + name)
	return nil
}
func (f *managedDockerFake) EnsureImage(_ context.Context, image string) error {
	f.record("ensure-image:" + image)
	return nil
}
func (f *managedDockerFake) EnsureContainer(_ context.Context, spec providers.ContainerSpec) (providers.ContainerInfo, error) {
	f.record("ensure-container:" + spec.Name)
	f.spec = spec
	if f.state == "" {
		f.state = "running"
	}
	return providers.ContainerInfo{ID: "mysql-id", Name: spec.Name, State: f.state}, nil
}
func (f *managedDockerFake) ConnectNetwork(_ context.Context, container, network string) error {
	f.record("connect-network:" + container + ":" + network)
	return nil
}

func TestManagedMySQLEnsureUsesPersistentVolumeLoopbackAndSharedNetwork(t *testing.T) {
	store := &fakeSecretStore{values: map[string][]byte{}}
	docker := &managedDockerFake{}
	manager := NewManagedMySQLManager(docker, store, ManagedMySQLConfig{})
	if err := manager.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if docker.spec.Name != "devbox-mysql" || docker.spec.Image != "mysql:8.4" {
		t.Fatalf("unexpected managed MySQL spec: %+v", docker.spec)
	}
	if docker.spec.Volumes["devbox-mysql-data"] != "/var/lib/mysql" {
		t.Fatalf("persistent MySQL volume missing: %+v", docker.spec.Volumes)
	}
	if len(docker.spec.Networks) != 1 || docker.spec.Networks[0] != "devbox-apps" {
		t.Fatalf("shared network missing: %+v", docker.spec.Networks)
	}
	if len(docker.spec.PortBindings) != 1 || docker.spec.PortBindings[0].HostIP != "127.0.0.1" ||
		docker.spec.PortBindings[0].ContainerPort != 3306 {
		t.Fatalf("admin endpoint must be loopback-only: %+v", docker.spec.PortBindings)
	}
	if docker.spec.SensitiveEnvironment["MYSQL_ROOT_PASSWORD"] == "" {
		t.Fatal("root password was not injected through sensitive environment")
	}
	if docker.spec.RestartPolicy != "unless-stopped" {
		t.Fatalf("unexpected restart policy %q", docker.spec.RestartPolicy)
	}
}

func TestManagedMySQLRestartKeepsPersistentVolumeAndSecret(t *testing.T) {
	store := &fakeSecretStore{values: map[string][]byte{}}
	docker := &managedDockerFake{state: "running"}
	manager := NewManagedMySQLManager(docker, store, ManagedMySQLConfig{})
	first, err := manager.RootPassword(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstValue := string(first)
	clear(first)
	if err := manager.Action(context.Background(), "restart"); err != nil {
		t.Fatal(err)
	}
	second, err := manager.RootPassword(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer clear(second)
	if string(second) != firstValue {
		t.Fatal("managed MySQL root secret changed during restart")
	}
	joined := strings.Join(docker.calls, "\n")
	if !strings.Contains(joined, "ensure-volume:devbox-mysql-data") {
		t.Fatalf("persistent volume was not reconciled: %s", joined)
	}
	if strings.Contains(joined, "remove:") {
		t.Fatalf("restart must not remove the MySQL container or its persistent volume: %s", joined)
	}
	if !strings.Contains(joined, "restart:devbox-mysql") {
		t.Fatalf("managed MySQL was not restarted: %s", joined)
	}
}

package databases

import (
	"context"
	"strings"
	"testing"
)

func TestManagedPostgreSQLEnsureUsesPersistentVolumeAndSharedNetwork(t *testing.T) {
	store := &fakeSecretStore{values: map[string][]byte{}}
	docker := &managedDockerFake{}
	manager := NewManagedPostgreSQLManager(docker, store, ManagedPostgreSQLConfig{})

	if err := manager.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if docker.spec.Name != DefaultManagedPostgreSQLContainer || docker.spec.Image != DefaultManagedPostgreSQLImage {
		t.Fatalf("unexpected managed PostgreSQL spec: %+v", docker.spec)
	}
	if docker.spec.Volumes[DefaultManagedPostgreSQLVolume] != "/var/lib/postgresql/data" {
		t.Fatalf("persistent PostgreSQL volume missing: %+v", docker.spec.Volumes)
	}
	if len(docker.spec.Networks) != 1 || docker.spec.Networks[0] != DefaultManagedMySQLNetwork {
		t.Fatalf("shared network missing: %+v", docker.spec.Networks)
	}
	if docker.spec.SensitiveEnvironment["POSTGRES_PASSWORD"] == "" {
		t.Fatal("PostgreSQL admin password was not injected through sensitive environment")
	}
	if docker.spec.SensitiveEnvironment["POSTGRES_USER"] != "postgres" {
		t.Fatalf("unexpected PostgreSQL admin user: %+v", docker.spec.SensitiveEnvironment)
	}
	if docker.spec.RestartPolicy != "unless-stopped" {
		t.Fatalf("unexpected restart policy %q", docker.spec.RestartPolicy)
	}
	if len(docker.spec.PortBindings) != 0 || len(docker.spec.Ports) != 0 {
		t.Fatalf("PostgreSQL must not publish its application port to the host: %+v %+v", docker.spec.PortBindings, docker.spec.Ports)
	}
}

func TestManagedPostgreSQLApplicationEndpointUsesContainerDNS(t *testing.T) {
	manager := NewManagedPostgreSQLManager(&managedDockerFake{}, &fakeSecretStore{values: map[string][]byte{}}, ManagedPostgreSQLConfig{})
	endpoint := manager.ApplicationEndpoint()
	if endpoint.Host != DefaultManagedPostgreSQLContainer || endpoint.Port != 5432 {
		t.Fatalf("unexpected PostgreSQL application endpoint: %+v", endpoint)
	}
}

func TestManagedPostgreSQLUninstallRemovesContainerButKeepsPersistentVolume(t *testing.T) {
	store := &fakeSecretStore{values: map[string][]byte{}}
	docker := &managedDockerFake{state: "running", exists: true}
	manager := NewManagedPostgreSQLManager(docker, store, ManagedPostgreSQLConfig{})

	if err := manager.Action(context.Background(), "uninstall"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(docker.calls, "\n")
	if !strings.Contains(joined, "stop:"+DefaultManagedPostgreSQLContainer) {
		t.Fatalf("running PostgreSQL container was not stopped before uninstall: %s", joined)
	}
	if !strings.Contains(joined, "remove:"+DefaultManagedPostgreSQLContainer) {
		t.Fatalf("PostgreSQL container was not removed: %s", joined)
	}
	if strings.Contains(joined, "ensure-volume:") {
		t.Fatalf("uninstall must not recreate or delete the persistent volume: %s", joined)
	}
}

package databases

import (
	"context"
	"testing"
)

func TestDockerMySQLTarget(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantHost    string
		wantGateway bool
	}{
		{name: "ipv4 loopback", input: "127.0.0.1", wantHost: "host.docker.internal", wantGateway: true},
		{name: "localhost", input: "localhost", wantHost: "host.docker.internal", wantGateway: true},
		{name: "ipv6 loopback", input: "::1", wantHost: "host.docker.internal", wantGateway: true},
		{name: "managed mysql", input: "devbox-mysql", wantHost: "devbox-mysql", wantGateway: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, gateway := dockerMySQLTarget(tt.input)
			if host != tt.wantHost || gateway != tt.wantGateway {
				t.Fatalf("dockerMySQLTarget(%q) = (%q, %v), want (%q, %v)", tt.input, host, gateway, tt.wantHost, tt.wantGateway)
			}
		})
	}
}

func TestPHPMyAdminRuntimeUsesManagedMySQLContainer(t *testing.T) {
	manager := NewPHPMyAdminManager(PHPMyAdminConfig{
		MySQLHost: "devbox-mysql",
		MySQLPort: 3306,
		Network:   "devbox-apps",
	})
	environment, err := manager.requiredEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if environment["PMA_ARBITRARY"] != "1" {
		t.Fatalf("PMA_ARBITRARY = %q, want 1", environment["PMA_ARBITRARY"])
	}
	if environment["PMA_HOST"] != "devbox-mysql" || environment["PMA_PORT"] != "3306" {
		t.Fatalf("unexpected managed MySQL target: %#v", environment)
	}
	if _, exists := environment["PMA_HOSTS"]; exists {
		t.Fatalf("managed runtime must not inject legacy host database targets: %#v", environment)
	}

	args := manager.createArgs(environment)
	if containsAdjacent(args, "--add-host", "host.docker.internal:host-gateway") {
		t.Fatalf("managed phpMyAdmin must not require the Docker host gateway: %#v", args)
	}
	if !containsAdjacent(args, "--network", "devbox-apps") {
		t.Fatalf("phpMyAdmin create args lost managed MySQL network: %#v", args)
	}
}

func TestPHPMyAdminLegacyLoopbackTargetAddsGatewayOnlyWhenNeeded(t *testing.T) {
	manager := NewPHPMyAdminManager(PHPMyAdminConfig{
		MySQLHost: "127.0.0.1",
		MySQLPort: 3306,
	})
	environment, err := manager.requiredEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if environment["PMA_HOST"] != "host.docker.internal" || environment["PMA_PORT"] != "3306" {
		t.Fatalf("unexpected legacy host target: %#v", environment)
	}
	args := manager.createArgs(environment)
	if !containsAdjacent(args, "--add-host", "host.docker.internal:host-gateway") {
		t.Fatalf("loopback compatibility target requires host-gateway mapping: %#v", args)
	}
}

func containsAdjacent(values []string, first, second string) bool {
	for index := 0; index+1 < len(values); index++ {
		if values[index] == first && values[index+1] == second {
			return true
		}
	}
	return false
}

func TestPHPMyAdminAutoLoginWhenManagedMySQLPluginIsInstalled(t *testing.T) {
	docker := &managedDockerFake{state: "running", exists: true}
	store := &fakeSecretStore{values: map[string][]byte{
		managedMySQLSecretScope + ":" + managedMySQLSecretName: []byte("secret"),
	}}
	managed := NewManagedMySQLManager(docker, store, ManagedMySQLConfig{})
	manager := NewPHPMyAdminManager(PHPMyAdminConfig{
		MySQLHost:    "devbox-mysql",
		MySQLPort:    3306,
		Network:      "devbox-apps",
		ManagedMySQL: managed,
	})
	environment, err := manager.requiredEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if environment["PMA_ARBITRARY"] != "0" {
		t.Fatalf("PMA_ARBITRARY = %q, want 0", environment["PMA_ARBITRARY"])
	}
	if environment["PMA_USER"] != "root" {
		t.Fatalf("PMA_USER = %q, want root", environment["PMA_USER"])
	}
	if environment["PMA_PASSWORD_FILE"] != phpMyAdminManagedPasswordPath {
		t.Fatalf("PMA_PASSWORD_FILE = %q, want %q", environment["PMA_PASSWORD_FILE"], phpMyAdminManagedPasswordPath)
	}
	if _, exposed := environment["PMA_PASSWORD"]; exposed {
		t.Fatal("managed MySQL password must not be exposed in phpMyAdmin container environment")
	}
}

func TestPHPMyAdminKeepsLoginScreenWhenManagedMySQLPluginIsNotInstalled(t *testing.T) {
	docker := &managedDockerFake{}
	managed := NewManagedMySQLManager(docker, &fakeSecretStore{values: map[string][]byte{}}, ManagedMySQLConfig{})
	manager := NewPHPMyAdminManager(PHPMyAdminConfig{
		MySQLHost:    "devbox-mysql",
		MySQLPort:    3306,
		Network:      "devbox-apps",
		ManagedMySQL: managed,
	})
	environment, err := manager.requiredEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if environment["PMA_ARBITRARY"] != "1" {
		t.Fatalf("PMA_ARBITRARY = %q, want 1", environment["PMA_ARBITRARY"])
	}
	if _, present := environment["PMA_USER"]; present {
		t.Fatalf("PMA_USER must be absent when managed MySQL is not installed: %#v", environment)
	}
	if _, present := environment["PMA_PASSWORD_FILE"]; present {
		t.Fatalf("PMA_PASSWORD_FILE must be absent when managed MySQL is not installed: %#v", environment)
	}
}

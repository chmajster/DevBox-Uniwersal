package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateDevBoxComposeWritesRuntimeArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	port := 18080
	project := Project{
		ID:        "project-1",
		Name:      "Example",
		Slug:      "example",
		LocalPath: dir,
		Runtime:   "php",
		Port:      &port,
	}
	config := RuntimeContainerConfig{
		ProjectID:       project.ID,
		Runtime:         "php",
		RuntimeVersion:  "8.3",
		ContainerPolicy: ContainerPolicyGeneratedCompose,
		Modules:         []RuntimeModule{{Name: "pdo_mysql"}},
	}

	result, err := generateDevBoxCompose(project, config, dir, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if result.ComposePath != "compose.yaml" || result.DockerfilePath != ".devbox/Dockerfile" {
		t.Fatalf("unexpected generated paths: %+v", result)
	}
	if result.HostPort != port || result.ContainerPort != 8080 {
		t.Fatalf("unexpected generated ports: %+v", result)
	}

	compose, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		devBoxGeneratedMarker,
		"services:",
		"app:",
		"dockerfile: \".devbox/Dockerfile\"",
		"\"18080:8080\"",
		"io.devbox.project: \"project-1\"",
		"- type: bind\n        source: .\n        target: \"/app\"\n        read_only: false",
		"- type: volume\n        target: \"/app/vendor\"",
	} {
		if !strings.Contains(string(compose), expected) {
			t.Fatalf("generated compose is missing %q:\n%s", expected, compose)
		}
	}

	dockerfile, err := os.ReadFile(filepath.Join(dir, ".devbox", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{devBoxGeneratedMarker, "FROM php:8.3-cli-bookworm", "pdo_mysql", "EXPOSE 8080"} {
		if !strings.Contains(string(dockerfile), expected) {
			t.Fatalf("generated Dockerfile is missing %q:\n%s", expected, dockerfile)
		}
	}
}

func TestGenerateDevBoxComposeDoesNotOverwriteProjectCompose(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}
	existing := []byte("services:\n  custom:\n    image: nginx:alpine\n")
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, existing, 0o600); err != nil {
		t.Fatal(err)
	}

	project := Project{ID: "project-2", Name: "Example", Slug: "example", LocalPath: dir, Runtime: "php"}
	config := RuntimeContainerConfig{ProjectID: project.ID, Runtime: "php", ContainerPolicy: ContainerPolicyGeneratedCompose}
	if _, err := generateDevBoxCompose(project, config, dir, "abc123"); err == nil {
		t.Fatal("expected existing project Compose file to block generation")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(existing) {
		t.Fatal("existing project Compose file was modified")
	}
	if _, err := os.Stat(filepath.Join(dir, "compose.yaml")); !os.IsNotExist(err) {
		t.Fatalf("compose.yaml must not be created after conflict, stat err=%v", err)
	}
}

func TestGenerateDevBoxComposeCanRefreshOwnFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.js"), []byte("console.log('ok')"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := Project{ID: "project-3", Name: "Node", Slug: "node", LocalPath: dir, Runtime: "node"}
	config := RuntimeContainerConfig{ProjectID: project.ID, Runtime: "node", RuntimeVersion: "22", ContainerPolicy: ContainerPolicyGeneratedCompose}

	if _, err := generateDevBoxCompose(project, config, dir, "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := generateDevBoxCompose(project, config, dir, "second"); err != nil {
		t.Fatalf("DevBox must be able to refresh its own generated Compose files: %v", err)
	}
}

package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProjectDeploymentModes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	support, err := DetectProjectDeployment(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !support.Dockerfile || support.ComposeFile != "compose.yaml" {
		t.Fatalf("unexpected detection result: %#v", support)
	}
	if len(support.Modes) != 2 || support.Modes[0] != DeploymentModeDocker || support.Modes[1] != DeploymentModeDockerCompose {
		t.Fatalf("unexpected deployment modes: %#v", support.Modes)
	}
}

func TestDetectProjectDeploymentWithoutDockerFiles(t *testing.T) {
	support, err := DetectProjectDeployment(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(support.Modes) != 0 {
		t.Fatalf("unexpected deployment modes: %#v", support.Modes)
	}
}

package docker

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestValidationRejectsCLIInjectionCharacters(t *testing.T) {
	for _, value := range []string{"abc;rm", "../../etc", "name space", "-flag", "x\nnext"} {
		if err := validateContainerRef(value); err == nil {
			t.Fatalf("expected container ref %q to fail", value)
		}
	}
	for _, value := range []string{"nginx:latest;echo", "../image", "-it"} {
		if err := validateImageRef(value); err == nil {
			t.Fatalf("expected image ref %q to fail", value)
		}
	}
}

func TestDiscoverComposeProjectsAcceptsSupportedFileNames(t *testing.T) {
	root := t.TempDir()
	names := []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
	for i, fileName := range names {
		dir := filepath.Join(root, "project"+string(rune('a'+i)))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fileName), []byte("services: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := DiscoverComposeProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 4 {
		t.Fatalf("expected 4 compose projects, got %d", len(projects))
	}
}

func TestSafeChildRejectsTraversal(t *testing.T) {
	if _, err := safeChild(t.TempDir(), "../outside"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestComposeArgsAvoidUnsupportedProjectDirectoryFlag(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(config, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	args, err := composeArgs(dir, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "--project-directory") {
		t.Fatalf("compose args must not contain --project-directory: %v", args)
	}
	want := []string{"compose", "--project-name", "sample", "--file", config}
	if !slices.Equal(args, want) {
		t.Fatalf("unexpected compose args: got %v want %v", args, want)
	}
}

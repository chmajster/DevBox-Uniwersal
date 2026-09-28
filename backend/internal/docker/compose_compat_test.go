package docker

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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

	want := []string{"compose", "--project-name", "example-app", "--file", composeFile}
	if !slices.Equal(args, want) {
		t.Fatalf("composeArgs() = %#v, want %#v", args, want)
	}
}

package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRepositoryURL(t *testing.T) {
	valid := []string{"https://github.com/example/repo.git", "ssh://git@github.com/example/repo.git", "git@github.com:example/repo.git"}
	for _, value := range valid {
		if err := ValidateRepositoryURL(value); err != nil {
			t.Fatalf("ValidateRepositoryURL(%q) error = %v", value, err)
		}
	}
	invalid := []string{"", "http://github.com/example/repo.git", "https://token@github.com/example/repo.git", "git@github.com", "file:///tmp/repo"}
	for _, value := range invalid {
		if err := ValidateRepositoryURL(value); err == nil {
			t.Fatalf("ValidateRepositoryURL(%q) expected error", value)
		}
	}
}

func TestValidateBranch(t *testing.T) {
	for _, value := range []string{"main", "feature/projects-git", "release/1.2.3"} {
		if err := ValidateBranch(value); err != nil {
			t.Fatalf("ValidateBranch(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "../main", "feature//bad", "branch.lock", "bad@{ref"} {
		if err := ValidateBranch(value); err == nil {
			t.Fatalf("ValidateBranch(%q) expected error", value)
		}
	}
}

func TestPathValidation(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "existing")
	if err := os.Mkdir(child, 0o750); err != nil {
		t.Fatal(err)
	}
	resolved, err := ValidateExistingDirectory(child)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("expected absolute resolved path, got %q", resolved)
	}
	if _, err := ValidateExistingDirectory("../relative"); err == nil {
		t.Fatal("expected relative path rejection")
	}
	generated, err := SafeProjectPath(root, "my-app")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(generated) != root {
		t.Fatalf("generated path escaped root: %q", generated)
	}
	if _, err := SafeProjectPath(root, "../escape"); err == nil {
		t.Fatal("expected project path escape rejection")
	}
	if _, err := SafeWorkingDirectory(root, "../../escape"); err == nil {
		t.Fatal("expected working directory escape rejection")
	}
}

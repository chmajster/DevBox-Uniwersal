package projects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func TestGitCloneAndState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary unavailable")
	}
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(source, 0o750); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, source, "init", "-b", "main")
	runGitTest(t, source, "config", "user.name", "DevBox Test")
	runGitTest(t, source, "config", "user.email", "devbox@example.invalid")
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("hello\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, source, "add", "README.md")
	runGitTest(t, source, "commit", "-m", "initial")
	destination := filepath.Join(t.TempDir(), "clone")
	client := NewGitClient(nil)
	if err := client.Clone(ctx, providers.GitSource{RepositoryURL: source, Reference: "main", Destination: destination}); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	state, err := client.State(ctx, destination)
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if state.Branch != "main" || state.Commit == "" || state.Dirty {
		t.Fatalf("unexpected state: %+v", state)
	}
	if len(state.History) != 1 || state.History[0].Subject != "initial" {
		t.Fatalf("unexpected history: %+v", state.History)
	}
	if err := os.WriteFile(filepath.Join(destination, "dirty.txt"), []byte("dirty"), 0o640); err != nil {
		t.Fatal(err)
	}
	state, err = client.State(ctx, destination)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Dirty {
		t.Fatal("expected dirty working tree")
	}
}

func TestTrustedGitArgsMarksProjectAsSafeDirectory(t *testing.T) {
	dir := t.TempDir()
	resolved, args, err := trustedGitArgs(dir, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if evaluated, evalErr := filepath.EvalSymlinks(absolute); evalErr == nil {
		absolute = evaluated
	}
	if resolved != absolute {
		t.Fatalf("trustedGitArgs() dir = %q, want %q", resolved, absolute)
	}
	want := []string{"-c", "safe.directory=" + absolute, "status", "--porcelain"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("trustedGitArgs() = %#v, want %#v", args, want)
	}
}

func TestTrustedGitArgsLeavesCloneWithoutWorkTreeUntouched(t *testing.T) {
	resolved, args, err := trustedGitArgs("", "clone", "https://example.invalid/repo.git", "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "" {
		t.Fatalf("unexpected command directory: %q", resolved)
	}
	want := []string{"clone", "https://example.invalid/repo.git", "/tmp/repo"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("trustedGitArgs() = %#v, want %#v", args, want)
	}
}

func TestGitStateParsingAndSecretMasking(t *testing.T) {
	ahead, behind := parseAheadBehind("2 3")
	if ahead != 2 || behind != 3 {
		t.Fatalf("unexpected ahead/behind %d/%d", ahead, behind)
	}
	secret := "ghp_super_secret_value"
	masked := MaskSecrets("remote rejected "+secret, secret)
	if strings.Contains(masked, secret) || !strings.Contains(masked, "***") {
		t.Fatalf("secret was not masked: %q", masked)
	}
	urlSecret := "https://user:token-value@example.com/repo.git"
	maskedURL := MaskSecrets("fatal: remote " + urlSecret)
	if strings.Contains(maskedURL, "token-value") || !strings.Contains(maskedURL, "https://***@example.com/repo.git") {
		t.Fatalf("URL credential was not masked: %q", maskedURL)
	}
	sshURL := "ssh://git@example.com/repo.git"
	if got := MaskSecrets(sshURL); got != sshURL {
		t.Fatalf("username-only SSH URL should not be masked: %q", got)
	}
}
func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
}

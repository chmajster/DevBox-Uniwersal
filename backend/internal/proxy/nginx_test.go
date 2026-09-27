package proxy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type fakeNginxRunner struct {
	failCandidate bool
	failGlobal    bool
	calls         [][]string
}

func (f *fakeNginxRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	copyArgs := append([]string(nil), args...)
	f.calls = append(f.calls, copyArgs)
	if f.failCandidate && len(args) >= 2 && args[0] == "-t" && args[1] == "-c" {
		return "candidate invalid", errors.New("exit status 1")
	}
	if f.failGlobal && len(args) == 1 && args[0] == "-t" {
		return "global invalid", errors.New("exit status 1")
	}
	if len(args) == 1 && args[0] == "-v" {
		return "nginx version: nginx/1.26.0", nil
	}
	return "", nil
}

func TestNginxConfigGeneration(t *testing.T) {
	provider := NewNginxProvider(NginxOptions{})
	config, err := provider.Render(providers.ProxyRoute{
		Domain:   "cloudportal.devbox.local",
		Upstream: "http://127.0.0.1:8010",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"server_name cloudportal.devbox.local;",
		"proxy_pass http://127.0.0.1:8010;",
		"proxy_set_header X-Forwarded-For",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("generated config missing %q:\n%s", expected, config)
		}
	}
}

func TestNginxFailedCandidateDoesNotActivate(t *testing.T) {
	available, enabled, oldPath := nginxTestLayout(t)
	runner := &fakeNginxRunner{failCandidate: true}
	provider := NewNginxProvider(NginxOptions{Binary: "nginx", SitesAvailable: available, SitesEnabled: enabled})
	provider.runner = runner

	err := provider.Apply(context.Background(), providers.ProxyRoute{
		Domain:   "cloudportal.devbox.local",
		Upstream: "http://127.0.0.1:8010",
	})
	if err == nil {
		t.Fatal("Apply() expected candidate validation error")
	}
	content, readErr := os.ReadFile(oldPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "old config\n" {
		t.Fatalf("site changed after failed candidate test: %q", string(content))
	}
	if containsReload(runner.calls) {
		t.Fatal("reload must not run for failed candidate")
	}
}

func TestNginxFailedGlobalValidationRollsBack(t *testing.T) {
	available, enabled, oldPath := nginxTestLayout(t)
	runner := &fakeNginxRunner{failGlobal: true}
	provider := NewNginxProvider(NginxOptions{Binary: "nginx", SitesAvailable: available, SitesEnabled: enabled})
	provider.runner = runner

	err := provider.Apply(context.Background(), providers.ProxyRoute{
		Domain:   "cloudportal.devbox.local",
		Upstream: "http://127.0.0.1:8010",
	})
	if err == nil {
		t.Fatal("Apply() expected active configuration validation error")
	}
	content, readErr := os.ReadFile(oldPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "old config\n" {
		t.Fatalf("rollback did not restore previous config: %q", string(content))
	}
	enabledPath := filepath.Join(enabled, "cloudportal.devbox.local.conf")
	target, err := os.Readlink(enabledPath)
	if err != nil {
		t.Fatalf("enabled symlink not restored: %v", err)
	}
	if target != oldPath {
		t.Fatalf("enabled symlink target = %q, want %q", target, oldPath)
	}
	if containsReload(runner.calls) {
		t.Fatal("reload must not run when global validation fails")
	}
}

func nginxTestLayout(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	available := filepath.Join(root, "sites-available")
	enabled := filepath.Join(root, "sites-enabled")
	if err := os.MkdirAll(available, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(available, "cloudportal.devbox.local.conf")
	if err := os.WriteFile(oldPath, []byte("old config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldPath, filepath.Join(enabled, "cloudportal.devbox.local.conf")); err != nil {
		t.Fatal(err)
	}
	return available, enabled, oldPath
}

func containsReload(calls [][]string) bool {
	for _, args := range calls {
		if len(args) == 2 && args[0] == "-s" && args[1] == "reload" {
			return true
		}
	}
	return false
}

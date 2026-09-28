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
	failCandidate   bool
	failGlobal      bool
	calls           [][]string
	candidateConfig string
}

func (f *fakeNginxRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	copyArgs := append([]string(nil), args...)
	f.calls = append(f.calls, copyArgs)
	if len(args) >= 3 && args[0] == "-t" && args[1] == "-c" {
		if data, err := os.ReadFile(args[2]); err == nil {
			f.candidateConfig = string(data)
		}
	}
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

func TestNginxCandidateValidationUsesUnprivilegedUnixSocket(t *testing.T) {
	runner := &fakeNginxRunner{}
	provider := NewNginxProvider(NginxOptions{Binary: "nginx"})
	provider.runner = runner

	err := provider.TestRoute(context.Background(), providers.ProxyRoute{
		Domain:   "cloudportal.devbox.local",
		Upstream: "http://127.0.0.1:8010",
	})
	if err != nil {
		t.Fatalf("TestRoute() error = %v", err)
	}
	if !strings.Contains(runner.candidateConfig, "listen unix:") {
		t.Fatalf("candidate config does not use a Unix socket:\n%s", runner.candidateConfig)
	}
	if strings.Contains(runner.candidateConfig, "listen 80;") || strings.Contains(runner.candidateConfig, "listen [::]:80;") {
		t.Fatalf("candidate config attempts privileged TCP bind:\n%s", runner.candidateConfig)
	}
	if !strings.Contains(runner.candidateConfig, "proxy_pass http://127.0.0.1:8010;") {
		t.Fatalf("candidate config lost route upstream:\n%s", runner.candidateConfig)
	}

	production, err := provider.Render(providers.ProxyRoute{
		Domain:   "cloudportal.devbox.local",
		Upstream: "http://127.0.0.1:8010",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(production, "listen 80;") || !strings.Contains(production, "listen [::]:80;") {
		t.Fatalf("production config no longer publishes HTTP on port 80:\n%s", production)
	}
}

func TestNginxUsesPrivilegedHelperForGlobalValidationAndReload(t *testing.T) {
	available, enabled, _ := nginxTestLayout(t)
	runner := &fakeNginxRunner{}
	provider := NewNginxProvider(NginxOptions{
		Binary:         "nginx",
		SitesAvailable: available,
		SitesEnabled:   enabled,
		HelperBinary:   "/usr/local/lib/devbox/devbox-helper",
		SudoBinary:     "sudo",
	})
	provider.runner = runner

	err := provider.Apply(context.Background(), providers.ProxyRoute{
		Domain:   "cloudportal.devbox.local",
		Upstream: "http://127.0.0.1:8010",
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !containsExactCall(runner.calls, []string{"-n", "/usr/local/lib/devbox/devbox-helper", "validate-nginx"}) {
		t.Fatal("global validation did not use privileged helper")
	}
	if !containsExactCall(runner.calls, []string{"-n", "/usr/local/lib/devbox/devbox-helper", "reload-nginx"}) {
		t.Fatal("reload did not use privileged helper")
	}
	if containsReload(runner.calls) {
		t.Fatal("direct unprivileged nginx reload must not run when helper is configured")
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

func containsExactCall(calls [][]string, expected []string) bool {
	for _, args := range calls {
		if len(args) != len(expected) {
			continue
		}
		match := true
		for i := range args {
			if args[i] != expected[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

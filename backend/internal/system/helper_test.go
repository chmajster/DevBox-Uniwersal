package system

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHelperRejectsUnknownPackageAndService(t *testing.T) {
	h := NewPrivilegedHelperWithRunner(fakeRunner{})
	if !errors.Is(h.InstallPackage(context.Background(), "curl;sh"), ErrOperationNotAllowed) {
		t.Fatal("expected arbitrary package rejection")
	}
	if !errors.Is(h.RestartService(context.Background(), "ssh"), ErrOperationNotAllowed) {
		t.Fatal("expected non-whitelisted service rejection")
	}
}

func TestMySQLPackageAndServiceAreAllowlisted(t *testing.T) {
	if got := allowedPackages["mysql"]; got != "default-mysql-server" {
		t.Fatalf("mysql package mapping = %q, want default-mysql-server", got)
	}
	if got := allowedServices["mysql"]; got != "mysql.service" {
		t.Fatalf("mysql service mapping = %q, want mysql.service", got)
	}
}

func TestPostgreSQLPackageAndServiceAreAllowlisted(t *testing.T) {
	if got := allowedPackages["postgresql"]; got != "postgresql" {
		t.Fatalf("postgresql package mapping = %q, want postgresql", got)
	}
	if got := allowedServices["postgresql"]; got != "postgresql.service" {
		t.Fatalf("postgresql service mapping = %q, want postgresql.service", got)
	}
}

func TestValidateNginx(t *testing.T) {
	h := NewPrivilegedHelperWithRunner(fakeRunner{
		paths:   map[string]string{"nginx": "/usr/sbin/nginx"},
		outputs: map[string]string{"/usr/sbin/nginx": "configuration file is valid\n"},
		errors:  map[string]error{},
	})
	if err := h.ValidateNginx(context.Background()); err != nil {
		t.Fatalf("ValidateNginx() error = %v", err)
	}
}

type helperRecordingRunner struct {
	paths map[string]string
	calls [][]string
}

func (r *helperRecordingRunner) LookPath(file string) (string, error) {
	if path, ok := r.paths[file]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func (r *helperRecordingRunner) CombinedOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	return nil, nil
}

func TestConfigureApplicationDatabaseUsesControlledSystemdRun(t *testing.T) {
	runner := &helperRecordingRunner{paths: map[string]string{
		"systemd-run": "/usr/bin/systemd-run",
	}}
	h := NewPrivilegedHelperWithRunner(runner)
	if err := h.ConfigureApplicationDatabase(context.Background(), "mysql", 3307); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v, want one systemd-run invocation", runner.calls)
	}
	call := runner.calls[0]
	if len(call) < 9 || call[0] != "/usr/bin/systemd-run" {
		t.Fatalf("unexpected configure call: %#v", call)
	}
	script := call[len(call)-1]
	if !strings.Contains(script, "port=3307") || !strings.Contains(script, "bind-address=0.0.0.0") {
		t.Fatalf("MySQL application config missing from script: %s", script)
	}
}

func TestConfigureApplicationDatabaseRejectsUnsafeInput(t *testing.T) {
	h := NewPrivilegedHelperWithRunner(helperRecordingRunner{paths: map[string]string{"systemd-run": "/usr/bin/systemd-run"}})
	if err := h.ConfigureApplicationDatabase(context.Background(), "mysql;rm", 3307); !errors.Is(err, ErrOperationNotAllowed) {
		t.Fatalf("unsafe engine error = %v", err)
	}
	if err := h.ConfigureApplicationDatabase(context.Background(), "mysql", 22); !errors.Is(err, ErrOperationNotAllowed) {
		t.Fatalf("unsafe port error = %v", err)
	}
	if _, err := ParseApplicationDatabasePort("3307;id"); !errors.Is(err, ErrOperationNotAllowed) {
		t.Fatalf("unsafe port parse error = %v", err)
	}
}

func TestPostgreSQLApplicationDatabaseScriptUsesSelectedPort(t *testing.T) {
	script := postgreSQLApplicationDatabaseScript(5544)
	if !strings.Contains(script, "set port 5544") || !strings.Contains(script, "listen_addresses '*'") {
		t.Fatalf("PostgreSQL application config missing: %s", script)
	}
	if !strings.Contains(script, "scram-sha-256") {
		t.Fatalf("PostgreSQL application HBA policy missing: %s", script)
	}
}

func TestValidateNginxEscapesDevBoxSystemdSandbox(t *testing.T) {
	runner := &helperRecordingRunner{paths: map[string]string{
		"nginx":       "/usr/sbin/nginx",
		"systemd-run": "/usr/bin/systemd-run",
	}}
	h := NewPrivilegedHelperWithRunner(runner)
	if err := h.ValidateNginx(context.Background()); err != nil {
		t.Fatalf("ValidateNginx() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v, want one systemd-run invocation", runner.calls)
	}
	got := runner.calls[0]
	want := []string{
		"/usr/bin/systemd-run", "--quiet", "--wait", "--pipe", "--collect", "/usr/sbin/nginx", "-t",
	}
	if len(got) != len(want) {
		t.Fatalf("systemd-run args = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("systemd-run args = %#v, want %#v", got, want)
		}
	}
}

func TestValidateDevBoxEnv(t *testing.T) {
	good := "DEVBOX_HTTP_ADDR=127.0.0.1:8787\nDEVBOX_DATABASE_PATH=/var/lib/devbox/devbox.db\n"
	if err := ValidateDevBoxEnv(good); err != nil {
		t.Fatalf("valid env rejected: %v", err)
	}
	bad := "LD_PRELOAD=/tmp/pwn.so\n"
	if !errors.Is(ValidateDevBoxEnv(bad), ErrOperationNotAllowed) {
		t.Fatalf("unsafe key was not rejected")
	}
	if err := ValidateDevBoxEnv("DEVBOX_HTTP_ADDR=$(id)\n"); err == nil {
		t.Fatal("shell expression was not rejected")
	}
}

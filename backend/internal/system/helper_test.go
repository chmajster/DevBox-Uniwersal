package system

import (
	"context"
	"errors"
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

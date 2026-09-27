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

package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DEVBOX_SESSION_TTL", "24h")
	t.Setenv("DEVBOX_COOKIE_SECURE", "false")
	t.Setenv("DEVBOX_HTTP_ADDR", "127.0.0.1:8787")
	t.Setenv("DEVBOX_DATABASE_PATH", "./data/devbox.db")
	t.Setenv("DEVBOX_MIGRATIONS_DIR", "./migrations")
	t.Setenv("DEVBOX_BOOTSTRAP_ADMIN_USERNAME", "")
	t.Setenv("DEVBOX_BOOTSTRAP_ADMIN_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8787" {
		t.Fatalf("unexpected HTTPAddr: %s", cfg.HTTPAddr)
	}
}

func TestLoadAllowsPasswordlessBootstrapAdminByDefault(t *testing.T) {
	t.Setenv("DEVBOX_AUTH_DISABLED", "true")
	t.Setenv("DEVBOX_BOOTSTRAP_ADMIN_USERNAME", "admin")
	t.Setenv("DEVBOX_BOOTSTRAP_ADMIN_PASSWORD", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.AuthDisabled {
		t.Fatal("expected passwordless auth to be enabled")
	}
}

func TestLoadRejectsPartialBootstrapCredentialsWhenAuthEnabled(t *testing.T) {
	t.Setenv("DEVBOX_AUTH_DISABLED", "false")
	t.Setenv("DEVBOX_BOOTSTRAP_ADMIN_USERNAME", "admin")
	t.Setenv("DEVBOX_BOOTSTRAP_ADMIN_PASSWORD", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for partial bootstrap credentials")
	}
}

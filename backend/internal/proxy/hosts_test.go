package proxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostsManagerAddsAndRemovesManagedEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewFileHostsManager(path)
	change, err := manager.Ensure(context.Background(), "cloudportal.devbox.local", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !change.Applied {
		t.Fatalf("expected applied change: %+v", change)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cloudportal.devbox.local\t# devbox") {
		t.Fatalf("managed hosts entry missing: %s", data)
	}
	if _, err := manager.Remove(context.Background(), "cloudportal.devbox.local"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cloudportal.devbox.local") {
		t.Fatalf("managed hosts entry was not removed: %s", data)
	}
}

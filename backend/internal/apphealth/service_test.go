package apphealth

import (
	"database/sql"
	"testing"
)

func TestResolveTargetRelativeUsesProjectPort(t *testing.T) {
	kind, target, err := resolveTarget("/health", sql.NullInt64{Int64: 8123, Valid: true})
	if err != nil {
		t.Fatal(err)
	}
	if kind != "http" || target != "http://127.0.0.1:8123/health" {
		t.Fatalf("unexpected target: %s %s", kind, target)
	}
}

func TestResolveTargetRejectsRelativeWithoutPort(t *testing.T) {
	if _, _, err := resolveTarget("/health", sql.NullInt64{}); err == nil {
		t.Fatal("expected relative target without project port to fail")
	}
}

func TestResolveTargetTCP(t *testing.T) {
	kind, target, err := resolveTarget("tcp://127.0.0.1:3306", sql.NullInt64{})
	if err != nil {
		t.Fatal(err)
	}
	if kind != "tcp" || target != "127.0.0.1:3306" {
		t.Fatalf("unexpected target: %s %s", kind, target)
	}
}

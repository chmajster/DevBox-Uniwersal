package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadProgressFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status")
	raw := "STATE=running\nPERCENT=68\nSTAGE=frontend\nMESSAGE=Build frontendu\nCURRENT_VERSION=abc\nTARGET_VERSION=def\nSTARTED_AT=2026-09-28T20:00:00Z\nUPDATED_AT=2026-09-28T20:01:00Z\nFINISHED_AT=\nERROR=\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	progress, err := readProgressFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State != "running" || progress.Percent != 68 || progress.Stage != "frontend" {
		t.Fatalf("unexpected progress: %#v", progress)
	}
	if progress.TargetVersion != "def" || progress.Message != "Build frontendu" {
		t.Fatalf("unexpected metadata: %#v", progress)
	}
}

func TestReadProgressFileClampsPercent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status")
	if err := os.WriteFile(path, []byte("STATE=running\nPERCENT=150\nSTAGE=backend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	progress, err := readProgressFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Percent != 100 {
		t.Fatalf("percent=%d want=100", progress.Percent)
	}
}

func TestProgressActive(t *testing.T) {
	for _, item := range []struct {
		state string
		want  bool
	}{
		{"starting", true},
		{"running", true},
		{"succeeded", false},
		{"failed", false},
		{"no_update", false},
		{"idle", false},
	} {
		if got := (Progress{State: item.state}).Active(); got != item.want {
			t.Fatalf("state=%s active=%v want=%v", item.state, got, item.want)
		}
	}
}

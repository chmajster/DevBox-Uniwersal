package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadProgressFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-status")
	raw := "STATE=failed\nPERCENT=68\nSTAGE=frontend\nMESSAGE=Build frontendu\nCURRENT_VERSION=abc\nTARGET_VERSION=def\nSTARTED_AT=2026-09-28T20:00:00Z\nUPDATED_AT=2026-09-28T20:01:00Z\nFINISHED_AT=2026-09-28T20:01:01Z\nERROR=npm build failed\nEXIT_CODE=2\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	progress, err := readProgressFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if progress.State != "failed" || progress.Percent != 68 || progress.Stage != "frontend" {
		t.Fatalf("unexpected progress: %#v", progress)
	}
	if progress.TargetVersion != "def" || progress.Message != "Build frontendu" || progress.Error != "npm build failed" {
		t.Fatalf("unexpected metadata: %#v", progress)
	}
	if progress.ExitCode == nil || *progress.ExitCode != 2 {
		t.Fatalf("unexpected exit code: %#v", progress.ExitCode)
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

func TestUpdateServiceStateRunning(t *testing.T) {
	for _, item := range []struct {
		state string
		want  bool
	}{
		{"active", true},
		{"activating", true},
		{"reloading", true},
		{"deactivating", true},
		{"inactive", false},
		{"failed", false},
		{"unknown", false},
		{"", false},
	} {
		t.Run(item.state, func(t *testing.T) {
			if got := updateServiceStateRunning(item.state); got != item.want {
				t.Fatalf("state=%q running=%v want=%v", item.state, got, item.want)
			}
		})
	}
}


func TestReadUpdateLogTailLimitsAndRedacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devbox-update.log")
	raw := "old line\npassword=hunter2\nAuthorization: Bearer abc.def.ghi\nnpm ERR! build failed\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readUpdateLogTail(path, 3, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines=%d want=3: %#v", len(lines), lines)
	}
	if lines[0] != "password=[REDACTED]" {
		t.Fatalf("password was not redacted: %q", lines[0])
	}
	if lines[1] != "Authorization: Bearer [REDACTED]" {
		t.Fatalf("bearer token was not redacted: %q", lines[1])
	}
	if lines[2] != "npm ERR! build failed" {
		t.Fatalf("unexpected final line: %q", lines[2])
	}
}

func TestSanitizeUpdateLogLineRedactsMySQLPassword(t *testing.T) {
	got := sanitizeUpdateLogLine("mysql --password=supersecret -h db")
	if got != "mysql --password=[REDACTED] -h db" {
		t.Fatalf("got %q", got)
	}
}

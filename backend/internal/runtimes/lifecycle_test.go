package runtimes

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestTailLines(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		content string
		tail    int
		want    string
	}{
		{name: "trailing newline", content: "one\ntwo\nthree\n", tail: 2, want: "two\nthree\n"},
		{name: "no trailing newline", content: "one\ntwo\nthree", tail: 2, want: "two\nthree"},
		{name: "larger tail", content: "one\ntwo", tail: 5, want: "one\ntwo"},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := string(tailLines([]byte(testCase.content), testCase.tail)); got != testCase.want {
				t.Fatalf("tailLines() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestStaticRuntimeRestart(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	project := ProjectContext{
		ProjectID: "static-restart",
		WorkDir:   workDir,
		Config:    map[string]any{"port": port},
	}
	runtime := NewStaticRuntime()
	ctx := context.Background()
	if err := runtime.Start(ctx, project); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background(), project) })

	if err := runtime.Restart(ctx, project); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	status, err := runtime.Status(ctx, project)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != "running" {
		t.Fatalf("status = %q, want running", status.State)
	}
}

func TestMaskedEnvironment(t *testing.T) {
	t.Parallel()
	config := RuntimeConfig{
		Environment: map[string]string{
			"PUBLIC_VALUE": "visible",
			"API_TOKEN":    "must-not-leak",
		},
		SecretEnvironment: map[string]SecretReference{
			"DB_PASSWORD": {Name: "database-password"},
		},
	}
	masked := maskedEnvironment(config)
	if masked["PUBLIC_VALUE"] != "visible" {
		t.Fatalf("PUBLIC_VALUE = %q, want visible", masked["PUBLIC_VALUE"])
	}
	if masked["API_TOKEN"] != "********" {
		t.Fatalf("API_TOKEN was not masked")
	}
	if masked["DB_PASSWORD"] != "********" {
		t.Fatalf("DB_PASSWORD was not masked")
	}
}

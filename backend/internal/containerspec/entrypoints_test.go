package containerspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoEntrypointsRequireRealMainFunctions(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                 "module example.test/app\ngo 1.23\n",
		"cmd/web/server.go":      "package main\nfunc main() {}",
		"cmd/worker/main.go":     "package main\nfunc main() {}",
		"cmd/library/main.go":    "package library\nfunc Main() {}",
		"vendor/example/main.go": "package main\nfunc main() {}",
	}
	for name, content := range files {
		file := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	options := StartOptions(root, "go", "", 8080, true)
	if len(options) != 2 || options[0].Command != "devbox-go-watch ./cmd/web" || options[1].Command != "devbox-go-watch ./cmd/worker" {
		t.Fatalf("options: %+v", options)
	}
	if command := SuggestedCommand(root, "go", "", 8080); command != "" {
		t.Fatalf("guessed ambiguous Go entrypoint: %s", command)
	}
}

func TestPythonEntrypointsUseDeclaredObjectsAndSelectedPort(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "api.py"), []byte("from fastapi import FastAPI\napi: FastAPI = FastAPI()\nother = FastAPI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	options := StartOptions(root, "python", "", 9000, true)
	if len(options) != 2 || options[0].Command != "python -m uvicorn web.api:api --host 0.0.0.0 --port 9000 --reload" {
		t.Fatalf("options: %+v", options)
	}
	for _, option := range options {
		if !strings.Contains(option.Command, "0.0.0.0") {
			t.Fatal(option)
		}
	}
	if err := os.Symlink(filepath.Join(root, "web", "api.py"), filepath.Join(root, "main.py")); err == nil {
		if len(StartOptions(root, "python", "", 9000, false)) != 2 {
			t.Fatal("followed a symlink entrypoint")
		}
	}
}

func TestDevelopmentReloadUpdatesSavedCommands(t *testing.T) {
	for _, test := range []struct {
		command, runtime, want string
		reload                 bool
	}{
		{"devbox-go-start ./cmd/web", "go", "devbox-go-watch ./cmd/web", true},
		{"devbox-go-watch .", "go", "devbox-go-start .", false},
		{"python -m uvicorn web:api --host 0.0.0.0 --port 9000", "python", "python -m uvicorn web:api --host 0.0.0.0 --port 9000 --reload", true},
		{"python custom.py", "python", "python custom.py", true},
	} {
		args, err := ParseStartCommand(test.command)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(ApplyDevelopmentReload(args, test.runtime, test.reload), " "); got != test.want {
			t.Fatalf("%s -> %s want %s", test.command, got, test.want)
		}
	}
}

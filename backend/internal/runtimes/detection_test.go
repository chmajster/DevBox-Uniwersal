package runtimes

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

func TestRuntimeDetectorsFromFixtures(t *testing.T) {
	t.Parallel()
	registry := NewDefaultRegistry()
	cases := []struct {
		name      string
		fixture   string
		runtime   string
		framework string
		minScore  int
	}{
		{name: "static", fixture: "static", runtime: "static", framework: "Static", minScore: 50},
		{name: "laravel", fixture: "php-laravel", runtime: "php", framework: "Laravel", minScore: 100},
		{name: "symfony", fixture: "php-symfony", runtime: "php", framework: "Symfony", minScore: 98},
		{name: "fastapi", fixture: "python-fastapi", runtime: "python", framework: "FastAPI", minScore: 96},
		{name: "django", fixture: "python-django", runtime: "python", framework: "Django", minScore: 100},
		{name: "go", fixture: "go", runtime: "go", framework: "Go", minScore: 96},
		{name: "vite", fixture: "node-vite", runtime: "node", framework: "Vite", minScore: 96},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runtime, ok := registry.Get(testCase.runtime)
			if !ok {
				t.Fatalf("runtime %q is not registered", testCase.runtime)
			}
			project := ProjectContext{ProjectID: testCase.name, WorkDir: filepath.Join("testdata", testCase.fixture)}
			detection, err := runtime.Detect(context.Background(), project)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if !detection.Detected {
				t.Fatalf("Detect() did not detect runtime %q", testCase.runtime)
			}
			if detection.Runtime != testCase.runtime {
				t.Fatalf("runtime = %q, want %q", detection.Runtime, testCase.runtime)
			}
			if framework := detectionString(detection, "framework"); framework != testCase.framework {
				t.Fatalf("framework = %q, want %q", framework, testCase.framework)
			}
			if confidence := detectionConfidence(detection); confidence < testCase.minScore {
				t.Fatalf("confidence = %d, want >= %d", confidence, testCase.minScore)
			}
			if len(detectionStrings(detection, "detected_files")) == 0 {
				t.Fatal("detected_files is empty")
			}
		})
	}
}

func TestPythonPackageManagersFromFixtures(t *testing.T) {
	t.Parallel()
	runtime := NewPythonRuntime(NewLocalProcessManager(), ExecRunner{})
	for _, testCase := range []struct {
		fixture string
		want    string
	}{
		{fixture: "python-fastapi", want: "pip"},
		{fixture: "python-uv", want: "uv"},
		{fixture: "python-poetry", want: "poetry"},
	} {
		project := ProjectContext{WorkDir: filepath.Join("testdata", testCase.fixture)}
		detection, err := runtime.Detect(context.Background(), project)
		if err != nil {
			t.Fatalf("%s: Detect() error = %v", testCase.fixture, err)
		}
		if got := detectionString(detection, "package_manager"); got != testCase.want {
			t.Fatalf("%s: package_manager = %q, want %q", testCase.fixture, got, testCase.want)
		}
	}
}

func TestPHPDetectorExtractsComposerExtensions(t *testing.T) {
	t.Parallel()
	runtime := NewPHPRuntime(NewLocalProcessManager(), ExecRunner{})
	project := ProjectContext{WorkDir: filepath.Join("testdata", "php-laravel")}
	detection, err := runtime.Detect(context.Background(), project)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	extensions, ok := detection.Metadata["required_extensions"].([]string)
	if !ok {
		t.Fatalf("required_extensions type = %T", detection.Metadata["required_extensions"])
	}
	for _, required := range []string{"ext-json", "ext-mbstring"} {
		if !slices.Contains(extensions, required) {
			t.Fatalf("required extension %q missing from %v", required, extensions)
		}
	}
}

func TestNodeDetectorUsesLockfilePackageManager(t *testing.T) {
	t.Parallel()
	runtime := NewNodeRuntime(NewLocalProcessManager(), ExecRunner{})
	project := ProjectContext{WorkDir: filepath.Join("testdata", "node-vite")}
	detection, err := runtime.Detect(context.Background(), project)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got := detectionString(detection, "package_manager"); got != "npm" {
		t.Fatalf("package_manager = %q, want npm", got)
	}
	if files := detectionStrings(detection, "detected_files"); !slices.Contains(files, "vite.config.ts") {
		t.Fatalf("vite.config.ts missing from detected files: %v", files)
	}
}

package containerspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionCommandsAndVersionedSource(t *testing.T) {
	for _, runtime := range []string{"php", "node", "python", "go", "static"} {
		t.Run(runtime, func(t *testing.T) {
			spec, err := GenerateManaged("audit-project", t.TempDir(), runtime, "", nil, "abc", 18080)
			if err != nil {
				t.Fatal(err)
			}
			image, err := BaseImage(runtime, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(spec.Dockerfile, strings.TrimPrefix(image, "library/")) {
				t.Fatalf("catalog/generator mismatch: %s", image)
			}
			build := "printf audit-build"
			if runtime == "go" {
				build = "go build -o /out/app ."
			}
			changed, err := WithExecution(spec, ExecutionOptions{SourceMode: "versioned"}, build, "printf audit-start", "/health?ready=1")
			if err != nil {
				t.Fatal(err)
			}
			if len(changed.BindMounts) != 0 || len(changed.AnonymousVolumes) != 0 {
				t.Fatal("versioned deployment mounts mutable source")
			}
			if !strings.Contains(changed.Dockerfile, build) || !strings.Contains(changed.Dockerfile, "audit-start") {
				t.Fatal("commands were ignored")
			}
			if changed.Fingerprint == spec.Fingerprint {
				t.Fatal("commands did not invalidate build fingerprint")
			}
			if changed.Healthcheck.Target != "/health?ready=1" {
				t.Fatal("lost configured healthcheck")
			}
		})
	}
}
func TestExecutionNodeArtifactsAndUID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"audit"}`), 0600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManaged("node-audit", dir, "node", "22", nil, "abc", 18080)
	if err != nil {
		t.Fatal(err)
	}
	got, err := WithExecution(spec, ExecutionOptions{UID: 2001, GID: 2002, WritablePaths: []string{"uploads"}}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mounts := strings.Join(got.AnonymousVolumes, "|")
	for _, expected := range []string{"/app/node_modules", "/app/dist", "/app/.next"} {
		if !strings.Contains(mounts, expected) {
			t.Errorf("missing protected artifact %s", expected)
		}
	}
	if got.User != "2001:2002" || len(got.WritablePaths) != 1 || got.WritablePaths[0] != "/app/uploads" {
		t.Fatalf("lost execution identity: %#v", got)
	}
}
func TestExecutionCGOAndValidation(t *testing.T) {
	spec, err := GenerateManaged("go-audit", t.TempDir(), "go", "", nil, "abc", 18080)
	if err != nil {
		t.Fatal(err)
	}
	got, err := WithExecution(spec, ExecutionOptions{CGO: true}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Dockerfile, "CGO_ENABLED=1") || !strings.Contains(got.Dockerfile, "FROM debian:bookworm-slim") || strings.Contains(got.Dockerfile, "FROM gcr.io/distroless") {
		t.Fatal(got.Dockerfile)
	}
	for _, path := range []string{"../data", "/etc", "cache/../../etc", ".env", ".git/hooks", "data;id"} {
		if ValidateExecution(ExecutionOptions{WritablePaths: []string{path}}) == nil {
			t.Errorf("unsafe path allowed: %s", path)
		}
	}
	for _, target := range []string{"https://example.com/health", "//example.com", "http://user:pass@localhost/", "http://localhost/\n"} {
		if ValidateHealthcheck(Healthcheck{Target: target}) == nil {
			t.Errorf("unsafe health target allowed: %q", target)
		}
	}
	gotURL, err := HealthcheckURL(Healthcheck{Target: "http://localhost:9999/ready?strict=1"}, 18080)
	if err != nil || gotURL != "http://127.0.0.1:18080/ready?strict=1" {
		t.Fatalf("wrong target %q: %v", gotURL, err)
	}
	if _, err := WithExecution(DeploymentSpec{Runtime: "custom"}, ExecutionOptions{}, "build", "", ""); err == nil {
		t.Fatal("silently ignored custom build")
	}
}

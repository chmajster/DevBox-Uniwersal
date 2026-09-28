package containerspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredContainerPortChangesGeneratedListenersAndImageFingerprint(t *testing.T) {
	for _, runtime := range []string{"php", "node", "python", "go", "static"} {
		t.Run(runtime, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0o644); err != nil {
				t.Fatal(err)
			}
			original, err := GenerateManaged("port-test", dir, runtime, "", nil, "", 8080)
			if err != nil {
				t.Fatal(err)
			}
			configured, err := WithContainerPort(original, 9090)
			if err != nil {
				t.Fatal(err)
			}
			if configured.ContainerPort != 9090 || configured.HostPort != 8080 {
				t.Fatalf("unexpected mapping %+v", configured)
			}
			if configured.Fingerprint == original.Fingerprint || configured.Image == original.Image {
				t.Fatal("listener change did not affect generated image")
			}
			if !strings.Contains(configured.Dockerfile, "EXPOSE 9090\n") || strings.Contains(configured.Dockerfile, "0.0.0.0:8080") || strings.Contains(configured.Dockerfile, "--port 8080") {
				t.Fatalf("stale listener in %s", configured.Dockerfile)
			}
			if runtime == "static" && !strings.Contains(configured.Dockerfile, "sed -i 's/8080/9090/g'") {
				t.Fatal("nginx listener was not changed")
			}
			for _, key := range []string{"PORT", "APP_PORT"} {
				if value, ok := configured.Environment[key]; ok && value != "9090" {
					t.Errorf("%s=%s", key, value)
				}
			}
			if original.Labels["io.devbox.fingerprint"] != original.Fingerprint {
				t.Fatal("input labels were mutated")
			}
			if configured.Labels["io.devbox.live-source"] != original.Labels["io.devbox.live-source"] || len(configured.BindMounts) != len(original.BindMounts) {
				t.Fatal("live code mounts changed")
			}
			changedHost := original
			changedHost.HostPort = 19090
			withHost, err := WithContainerPort(changedHost, 9090)
			if err != nil || withHost.Fingerprint != configured.Fingerprint {
				t.Fatalf("published port triggered an image rebuild: %v", err)
			}
		})
	}
}

func TestCustomContainerPortDoesNotRewriteApplicationDockerfile(t *testing.T) {
	dir := t.TempDir()
	content := "FROM nginx:alpine\nEXPOSE 80\n"
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := GenerateCustomDockerfile("custom-port-test", dir, 8080)
	if err != nil {
		t.Fatal(err)
	}
	if original.ContainerPort != 80 {
		t.Fatalf("EXPOSE detection = %d", original.ContainerPort)
	}
	configured, err := WithContainerPort(original, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if configured.ContainerPort != 3000 || configured.Fingerprint != original.Fingerprint {
		t.Fatal("custom image was unexpectedly rewritten")
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != content {
		t.Fatalf("source Dockerfile changed: %v", err)
	}
	for _, port := range []int{-1, 65536} {
		if _, err := WithContainerPort(original, port); err == nil {
			t.Fatalf("accepted %d", port)
		}
	}
	automatic, err := WithContainerPort(original, 0)
	if err != nil || automatic.ContainerPort != 80 {
		t.Fatalf("automatic detection was lost: %v", err)
	}
}

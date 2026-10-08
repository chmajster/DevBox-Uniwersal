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

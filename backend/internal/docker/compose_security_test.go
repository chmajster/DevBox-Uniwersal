package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestComposeRejectsHostAccessThroughBuildAndStorage(t *testing.T) {
	source, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "Dockerfile"), []byte("FROM alpine"), 0600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]map[string]any{
		"dockerfile":         {"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": source, "dockerfile": filepath.Join(outside, "Dockerfile")}}}},
		"additional context": {"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": source, "additional_contexts": map[string]string{"host": outside}}}}},
		"volume driver bind": {"services": map[string]any{"web": map[string]any{}}, "volumes": map[string]any{"host": map[string]any{"driver_opts": map[string]string{"type": "none", "o": "bind", "device": outside}}}},
		"secret file":        {"services": map[string]any{"web": map[string]any{}}, "secrets": map[string]any{"key": map[string]string{"file": filepath.Join(outside, "Dockerfile")}}},
		"build SSH":          {"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": source, "ssh": []string{"default"}}}}},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			data, _ := json.Marshal(config)
			if err := validateApplicationCompose(data, source); err == nil {
				t.Fatal("unmanaged host access accepted")
			}
		})
	}
	data, _ := json.Marshal(map[string]any{"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": source}, "volumes": []any{map[string]string{"type": "bind", "source": source, "target": "/app"}}}}, "volumes": map[string]any{"data": map[string]any{}}})
	if err := validateApplicationCompose(data, source); err != nil {
		t.Fatalf("authorized source rejected: %v", err)
	}
}

package containerspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStartCommandIsAnArgvWithoutShellExpansion(t *testing.T) {
	for _, test := range []struct {
		text string
		want []string
	}{
		{"npm run dev -- --host 0.0.0.0", []string{"npm", "run", "dev", "--", "--host", "0.0.0.0"}},
		{"python 'file with spaces.py'", []string{"python", "file with spaces.py"}},
		{"nginx -g 'daemon off;'", []string{"nginx", "-g", "daemon off;"}},
		{"./server --value '$HOME; echo nope'", []string{"./server", "--value", "$HOME; echo nope"}},
	} {
		args, err := ParseStartCommand(test.text)
		if err != nil || !reflect.DeepEqual(args, test.want) {
			t.Fatalf("%q: %v %v", test.text, args, err)
		}
	}
	for _, command := range []string{"npm start; touch /tmp/injected", "echo $(id)", "python app.py | cat", "python 'unclosed", "-bad", "python\napp.py"} {
		if _, err := ParseStartCommand(command); err == nil {
			t.Fatalf("accepted %q", command)
		}
	}
}

func TestRuntimeCatalogContainsRequestedVersionsAndIndependentImages(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.php"), []byte("<?php"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, runtime := range RuntimeCatalog() {
		if runtime.DefaultVersion != DefaultVersion(runtime.Name) {
			t.Fatal("divergent default")
		}
		for _, version := range runtime.Versions {
			if err := Validate(runtime.Name, version, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	a, err := GenerateManagedProfile("app-a", root, "php", "8.2", nil, "", 32781, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateManagedProfile("app-b", root, "php", "8.4", nil, "", 32782, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Image == b.Image || a.ContainerName == b.ContainerName || a.Version == b.Version {
		t.Fatal("application runtime identities overlap")
	}
	if a.BindMounts[root] != "/app" || b.BindMounts[root] != "/app" {
		t.Fatal("live source mount missing")
	}
}

func TestGeneratedWordPressConfigurationUsesEnvAndSourceOwner(t *testing.T) {
	root := t.TempDir()
	spec, err := GenerateManagedProfile("wordpress-test", root, "php", "8.4", nil, "", 32781, "wordpress")
	if err != nil {
		t.Fatal(err)
	}
	if spec.BindMounts[root] != "/var/www/html" || spec.ContainerPort != 80 {
		t.Fatal("WordPress source/port not configured")
	}
	for _, part := range []string{"getenv('WORDPRESS_DB_$key')", "posix_setgid", "posix_setuid", "fopen($path, 'x')", "HTTP_X_FORWARDED_PROTO"} {
		if !strings.Contains(wordpressInit, part) {
			t.Fatal("missing WordPress source/config safety", part)
		}
	}
	if !strings.Contains(spec.Dockerfile, "devbox-wordpress-entrypoint") || !strings.Contains(spec.Dockerfile, "-apache-bookworm") {
		t.Fatal("Apache/config entrypoint absent")
	}
}

func TestGeneratedNodeCommandHasValidJSONAndJavascriptQuotes(t *testing.T) {
	spec, err := GenerateManagedProfile("node-start", t.TempDir(), "node", "22", nil, "", 32781, "")
	if err != nil {
		t.Fatal(err)
	}
	var command []string
	for _, line := range strings.Split(spec.Dockerfile, "\n") {
		if strings.HasPrefix(line, "CMD ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "CMD ")), &command); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(command) != 3 || !strings.Contains(command[2], `require("./package.json")`) || strings.Contains(command[2], `require(\"`) {
		t.Fatal("Node startup contains literal escaped JS quotes", command)
	}
}

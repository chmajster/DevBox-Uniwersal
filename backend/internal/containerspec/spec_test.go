package containerspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateManagedPHPModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManaged("project-123", dir, "php", "8.3", []Module{{Name: "pdo_mysql"}, {Name: "mbstring"}}, "abc123", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ContainerPort != 8080 || spec.HostPort != 18080 {
		t.Fatalf("unexpected ports: %#v", spec)
	}
	for _, expected := range []string{"php:8.3-cli-bookworm", "pdo_mysql", "mbstring", "libonig-dev", "USER 10001"} {
		if !strings.Contains(spec.Dockerfile, expected) {
			t.Fatalf("Dockerfile does not contain %q:\n%s", expected, spec.Dockerfile)
		}
	}
	if !strings.HasPrefix(spec.Image, "devbox/runtime-project-123:") {
		t.Fatalf("unexpected image: %s", spec.Image)
	}
}

func TestPHPModuleCatalogIncludesContainerExtensions(t *testing.T) {
	items, err := Catalog("php")
	if err != nil {
		t.Fatal(err)
	}
	available := make(map[string]bool, len(items))
	for _, item := range items {
		available[item.Name] = true
	}
	for _, expected := range []string{"pgsql", "sqlite3", "ldap", "gmp", "imagick", "redis", "memcached", "xdebug"} {
		if !available[expected] {
			t.Fatalf("PHP module catalog is missing %q", expected)
		}
	}
}

func TestGenerateManagedPHPInstallsSelectedContainerModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManaged("project-php-modules", dir, "php", "8.3", []Module{
		{Name: "pgsql"},
		{Name: "sqlite3"},
		{Name: "ldap"},
		{Name: "imagick"},
		{Name: "redis"},
		{Name: "memcached"},
		{Name: "xdebug"},
	}, "abc123", 18080)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"libpq-dev",
		"libsqlite3-dev",
		"libldap2-dev",
		"libmagickwand-dev",
		"libmemcached-dev",
		"docker-php-ext-install -j$(nproc)",
		"pgsql pdo_pgsql",
		"sqlite3 pdo_sqlite",
		"docker-php-ext-configure ldap",
		"pecl install imagick && docker-php-ext-enable imagick",
		"pecl install redis && docker-php-ext-enable redis",
		"pecl install memcached && docker-php-ext-enable memcached",
		"pecl install xdebug && docker-php-ext-enable xdebug",
	} {
		if !strings.Contains(spec.Dockerfile, expected) {
			t.Fatalf("Dockerfile does not contain %q:\n%s", expected, spec.Dockerfile)
		}
	}
}

func TestGenerateManagedAddsLiveSourceMounts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte("{\"require\":{}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManaged("project-live", dir, "php", "8.3", nil, "abc123", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.BindMounts[dir]; got != "/app" {
		t.Fatalf("expected live source bind %s -> /app, got %q", dir, got)
	}
	foundVendor := false
	for _, target := range spec.AnonymousVolumes {
		if target == "/app/vendor" {
			foundVendor = true
			break
		}
	}
	if !foundVendor {
		t.Fatalf("expected /app/vendor dependency volume, got %#v", spec.AnonymousVolumes)
	}
	if spec.Labels["io.devbox.live-source"] != "true" {
		t.Fatalf("expected live-source label, got %#v", spec.Labels)
	}
}

func TestGenerateManagedSanitizesLegacyProjectIDForDockerReference(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManaged("Legacy___Project::ID///old", dir, "static", "", nil, "", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(spec.Image, "devbox/runtime-legacy-project-id-old:") {
		t.Fatalf("unexpected Docker image reference: %q", spec.Image)
	}
	if spec.ContainerName != "devbox-app-legacy-project-id-old" {
		t.Fatalf("unexpected Docker container name: %q", spec.ContainerName)
	}
}

func TestGenerateCustomDockerfileRejectsProjectIDWithoutDockerSafeCharacters(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM busybox\nEXPOSE 8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateCustomDockerfile("___///:::", dir, 18080); err == nil {
		t.Fatal("expected invalid legacy project id to be rejected before Docker invocation")
	}
}

func TestGenerateManagedRejectsUnknownModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateManaged("p1", dir, "php", "8.3", []Module{{Name: "imaginary"}}, "", 18080); err == nil {
		t.Fatal("expected unknown module to be rejected")
	}
}

func TestFingerprintChangesWithSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc main(){}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := GenerateManaged("p1", dir, "go", "1.23", nil, "", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\nfunc main(){println(1)}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := GenerateManaged("p1", dir, "go", "1.23", nil, "", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("fingerprint must change when build context changes")
	}
}

func TestFingerprintIgnoresDotEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("console.log('ok')"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("TOKEN=one"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := GenerateManaged("p1", dir, "node", "22", nil, "", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("TOKEN=two"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := GenerateManaged("p1", dir, "node", "22", nil, "", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatal(".env must be excluded from managed build fingerprint/context")
	}
}

func TestGenerateCustomDockerfileReadsExpose(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM busybox\nEXPOSE 9000\nCMD [\"httpd\",\"-f\",\"-p\",\"9000\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateCustomDockerfile("custom-1", dir, 19000)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ContainerPort != 9000 {
		t.Fatalf("expected EXPOSE 9000, got %d", spec.ContainerPort)
	}
}

func TestDatabaseSecretIsNeverWrittenToManagedDockerfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManaged("project-secret", dir, "php", "8.3", []Module{{Name: "pdo_mysql"}}, "abc123", 18080)
	if err != nil {
		t.Fatal(err)
	}
	secret := "never-write-this-database-secret"
	spec.SensitiveEnvironment = map[string]string{"DB_PASSWORD": secret}
	if strings.Contains(spec.Dockerfile, secret) || strings.Contains(spec.Dockerfile, "DB_PASSWORD") {
		t.Fatalf("runtime database secret leaked into generated Dockerfile:\n%s", spec.Dockerfile)
	}
}

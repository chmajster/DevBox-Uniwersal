package containerspec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
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
	for _, expected := range []string{"php:8.3-apache-bookworm", "pdo_mysql", "apache2-foreground"} {
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
		"libldap2-dev",
		"libmagickwand-dev",
		"libmemcached-dev",
		"docker-php-ext-install -j$(nproc)",
		"pgsql pdo_pgsql",
		"DEB_BUILD_MULTIARCH",
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
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := sourceContainerUser(info, "php"); want != "" && (spec.Environment["APACHE_RUN_USER"] != "devbox-source" || !strings.Contains(spec.Dockerfile, "--uid "+strings.Split(want, ":")[0])) {
		t.Fatalf("Apache worker does not use source owner: %v", spec.Environment)
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

func TestGenerateManagedDoesNotMapOwnerWithoutOwnerWriteAccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o570); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid == 0 {
		t.Skip("requires a non-root-owned source directory")
	}
	spec, err := GenerateManaged("project-group-write", dir, "php", "8.3", nil, "", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if spec.User != "" {
		t.Fatalf("owner UID/GID should not be selected without owner write and execute permissions: %q", spec.User)
	}
}

func TestGenerateManagedDoesNotLeaveTrailingSeparatorWhenUUIDIsTruncated(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManaged("12345678-1234-1234-1234-123456789abc", dir, "static", "", nil, "", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(spec.Image, "devbox/runtime-12345678-1234-1234-1234:") {
		t.Fatalf("UUID truncation produced an invalid Docker image reference: %q", spec.Image)
	}
	if strings.Contains(spec.Image, "-:") {
		t.Fatalf("Docker repository component must not end in a separator: %q", spec.Image)
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

func TestGenerateManagedRejectsUnknownModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateManaged("p1", dir, "php", "8.3", []Module{{Name: "imaginary"}}, "", 18080); err == nil {
		t.Fatal("expected unknown module to be rejected")
	}
}

func TestManagedFingerprintIgnoresLiveApplicationSourceChanges(t *testing.T) {
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
	if first.Fingerprint != second.Fingerprint {
		t.Fatal("live source changes must not invalidate the dependency runtime image")
	}
}

func TestAutoContainerFingerprintIgnoresApplicationSourceChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.php")
	if err := os.WriteFile(path, []byte("<?php echo 'first';"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := GenerateManagedProfile("p1", dir, "php", "8.3", nil, "", 18080, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<?php echo 'second';"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := GenerateManagedProfile("p1", dir, "php", "8.3", nil, "", 18080, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatal("Auto Container source edits must not rebuild the runtime image")
	}
}

func TestAutoGoUsesLiveMountedSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test/app\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main(){}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManagedProfile("p1", dir, "go", "1.23", nil, "", 18080, "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.BindMounts[dir] != "/app" || !strings.Contains(spec.Dockerfile, `CMD ["devbox-go-start","."]`) || strings.Contains(spec.Dockerfile, "COPY . .") {
		t.Fatalf("Go Auto Container must run its live mount: mount=%v\n%s", spec.BindMounts, spec.Dockerfile)
	}
}

func TestFingerprintChangesWithDependencyManifest(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(manifest, []byte("module example.test/app\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := GenerateManagedProfile("p1", dir, "go", "1.23", nil, "", 18080, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("module example.test/app\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := GenerateManagedProfile("p1", dir, "go", "1.23", nil, "", 18080, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("dependency manifest changes must rebuild the runtime image")
	}
}

func TestGenerateManagedWordPressProfile(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"wp-admin", "wp-content", "wp-includes"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "wp-login.php"), []byte("<?php echo 'wordpress';"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec, err := GenerateManagedProfile("wordpress-test", dir, "php", "8.3", nil, "", 18080, "wordpress")
	if err != nil {
		t.Fatal(err)
	}
	if spec.ContainerPort != 80 {
		t.Fatalf("container port = %d, want 80", spec.ContainerPort)
	}
	if spec.BindMounts[dir] != "/var/www/html" {
		t.Fatalf("mount = %#v", spec.BindMounts)
	}
	if len(spec.Capabilities) == 0 || !strings.Contains(spec.Dockerfile, "php:8.3-apache-bookworm") {
		t.Fatalf("WordPress must use PHP Apache: %#v\n%s", spec.Capabilities, spec.Dockerfile)
	}
	for _, extension := range []string{"mysqli", "pdo_mysql", "gd", "zip", "intl", "opcache", "exif"} {
		if !strings.Contains(spec.Dockerfile, extension) {
			t.Errorf("WordPress image must include %s", extension)
		}
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

func TestGenerateManagedPHPAutoDetectsComposerExtensions(t *testing.T) {
	dir := t.TempDir()
	composer := `{
		"require": {
			"php": "^8.2",
			"ext-pdo": "*",
			"ext-mbstring": "*",
			"ext-dom": "*",
			"ext-curl": "*",
			"ext-ldap": "*",
			"ext-zip": "*",
			"ext-gd": "*"
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec, err := GenerateManaged("php-composer-auto", dir, "php", "8.3", nil, "abc123", 18080)
	if err != nil {
		t.Fatal(err)
	}

	for _, expected := range []string{
		"libldap2-dev",
		"libzip-dev",
		"libpng-dev",
		"ldap",
		"zip",
		"gd",
		"COMPOSER_ALLOW_SUPERUSER=1",
		"--no-progress --no-ansi",
	} {
		if !strings.Contains(spec.Dockerfile, expected) {
			t.Fatalf("auto-detected Composer extension did not add %q:\n%s", expected, spec.Dockerfile)
		}
	}
}

func TestGenerateManagedPHPComposerModulesMergeWithConfiguredModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"ext-mbstring":"*","ext-zip":"*"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php echo 'ok';"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec, err := GenerateManaged("php-composer-merge", dir, "php", "8.3", []Module{{Name: "mbstring"}, {Name: "pdo_mysql"}}, "abc123", 18080)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(spec.Dockerfile, "docker-php-ext-install -j$(nproc)") != 1 {
		t.Fatalf("expected one PHP extension installation command:\n%s", spec.Dockerfile)
	}
	for _, expected := range []string{"pdo_mysql", "zip"} {
		if !strings.Contains(spec.Dockerfile, expected) {
			t.Fatalf("merged module set is missing %q:\n%s", expected, spec.Dockerfile)
		}
	}
}

func TestGenericManagedProfilesMatchDefaultSpecification(t *testing.T) {
	for _, runtime := range []string{"php", "node", "python", "go", "static"} {
		t.Run(runtime, func(t *testing.T) {
			dir := t.TempDir()
			defaultSpec, err := GenerateManagedProfile("generic-test", dir, runtime, DefaultVersion(runtime), nil, "", 18080, "")
			if err != nil {
				t.Fatal(err)
			}
			namedSpec, err := GenerateManagedProfile("generic-test", dir, runtime, DefaultVersion(runtime), nil, "", 18080, "generic_"+runtime)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(defaultSpec, namedSpec) {
				t.Fatal("persisted generic profile changed the generated specification")
			}
		})
	}
}

func TestManagedProfileRejectsMismatchedAndUnknownProfiles(t *testing.T) {
	for _, profile := range []string{"generic_node", "generic_php_extra", "unknown"} {
		t.Run(profile, func(t *testing.T) {
			_, err := GenerateManagedProfile("invalid-profile", t.TempDir(), "php", "8.3", nil, "", 18080, profile)
			if err == nil || !strings.Contains(err.Error(), "unsupported managed runtime profile") {
				t.Fatalf("expected profile validation, got %v", err)
			}
		})
	}
}

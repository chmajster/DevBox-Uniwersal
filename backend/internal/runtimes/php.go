package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type composerManifest struct {
	Require map[string]string `json:"require"`
}

type PHPRuntime struct {
	base runtimeBase
}

func NewPHPRuntime(processes providers.ProcessManager, runner CommandRunner) *PHPRuntime {
	return &PHPRuntime{base: newRuntimeBase(processes, runner)}
}

func (r *PHPRuntime) Name() string { return "php" }

func (r *PHPRuntime) Inspect(ctx context.Context) RuntimeInfo {
	php := inspectExecutable(ctx, "php", []string{"php"}, "--version")
	fpm := inspectExecutable(ctx, "php-fpm", []string{"php-fpm", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1"}, "--version")
	composer := inspectExecutable(ctx, "composer", []string{"composer"}, "--version")
	return aggregateRuntimeInfo(r.Name(), php, []DependencyInfo{fpm}, []DependencyInfo{composer})
}

func (r *PHPRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}

	files := existingFiles(project.WorkDir, "artisan", "composer.json", "index.php", "symfony.lock")
	if len(files) == 0 {
		return Detection{Runtime: r.Name()}, nil
	}

	manifest, err := loadComposerManifest(project.WorkDir)
	if err != nil {
		return Detection{}, err
	}
	framework := "PHP"
	confidence := 70
	switch {
	case fileExists(project.WorkDir, "artisan") || composerRequires(manifest, "laravel/framework"):
		framework = "Laravel"
		confidence = 100
	case fileExists(project.WorkDir, "symfony.lock") || composerRequires(manifest, "symfony/framework-bundle"):
		framework = "Symfony"
		confidence = 98
	case fileExists(project.WorkDir, "composer.json"):
		confidence = 85
	}

	extensions := composerExtensions(manifest)
	version := ""
	if manifest != nil {
		version = manifest.Require["php"]
	}
	detection := newDetection(
		r.Name(),
		framework,
		confidence,
		files,
		"composer install --no-interaction --prefer-dist && composer dump-autoload -o",
		"php-fpm -F -y .devbox/php/php-fpm.conf",
		map[string]any{
			"required_extensions": extensions,
			"document_root":       phpDocumentRoot(project.WorkDir, framework),
		},
	)
	detection.Version = version
	return detection, nil
}

func (r *PHPRuntime) Validate(ctx context.Context, project ProjectContext) (ValidationResult, error) {
	result := ValidationResult{Valid: true}
	if err := validateWorkDir(project); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	php, err := projectExecutable(project, "php", "php")
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if _, err := projectExecutable(project, "php-fpm", "php-fpm", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1"); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, "PHP-FPM is required: "+err.Error())
	}

	manifest, err := loadComposerManifest(project.WorkDir)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if manifest != nil {
		if _, _, err := composerInvocation(project); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, "Composer is required for composer.json projects: "+err.Error())
		}
		availableExtensions, extensionErr := phpExtensions(ctx, php)
		if extensionErr != nil {
			result.Valid = false
			result.Errors = append(result.Errors, extensionErr.Error())
		} else {
			for _, extension := range composerExtensions(manifest) {
				if !availableExtensions[strings.TrimPrefix(extension, "ext-")] {
					result.Valid = false
					result.Errors = append(result.Errors, "missing PHP extension: "+extension)
				}
			}
		}
	}
	if _, ok := projectPort(project); !ok {
		result.Warnings = append(result.Warnings, "runtime port is not configured; PHP-FPM Start requires a project port")
	}
	return result, nil
}

func (r *PHPRuntime) InstallDependencies(ctx context.Context, project ProjectContext) error {
	if !fileExists(project.WorkDir, "composer.json") {
		return nil
	}
	command, prefix, err := composerInvocation(project)
	if err != nil {
		return err
	}
	args := append(prefix, "install", "--no-interaction", "--prefer-dist")
	return r.base.runner.Run(ctx, command, args, project.WorkDir, project.Environment)
}

func (r *PHPRuntime) Build(ctx context.Context, project ProjectContext) error {
	if !fileExists(project.WorkDir, "composer.json") {
		return nil
	}
	command, prefix, err := composerInvocation(project)
	if err != nil {
		return err
	}
	args := append(prefix, "dump-autoload", "-o", "--no-interaction")
	return r.base.runner.Run(ctx, command, args, project.WorkDir, project.Environment)
}

func (r *PHPRuntime) Start(_ context.Context, project ProjectContext) error {
	if err := validateWorkDir(project); err != nil {
		return err
	}
	port, ok := projectPort(project)
	if !ok {
		return fmt.Errorf("runtime port is not configured")
	}
	fpm, err := projectExecutable(project, "php-fpm", "php-fpm", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1")
	if err != nil {
		return err
	}

	configDir := filepath.Join(project.WorkDir, ".devbox", "php")
	logDir := filepath.Join(project.WorkDir, ".devbox", "logs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create PHP runtime directory: %w", err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create PHP log directory: %w", err)
	}
	configPath := filepath.Join(configDir, "php-fpm.conf")
	config := fmt.Sprintf("[global]\ndaemonize = no\nerror_log = %s\n\n[www]\nlisten = 127.0.0.1:%d\npm = ondemand\npm.max_children = 4\npm.process_idle_timeout = 10s\nclear_env = no\ncatch_workers_output = yes\nchdir = %s\n", filepath.Join(logDir, "php-fpm-error.log"), port, project.WorkDir)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return fmt.Errorf("write PHP-FPM config: %w", err)
	}
	return r.base.start(project, fpm, []string{"-F", "-y", configPath}, nil)
}

func (r *PHPRuntime) Stop(ctx context.Context, project ProjectContext) error {
	return r.base.stop(ctx, project)
}

func (r *PHPRuntime) Restart(ctx context.Context, project ProjectContext) error {
	return r.base.restart(ctx, project)
}

func (r *PHPRuntime) Status(ctx context.Context, project ProjectContext) (ProcessStatus, error) {
	return r.base.status(ctx, project)
}

func (r *PHPRuntime) Logs(ctx context.Context, project ProjectContext, options LogOptions) (io.ReadCloser, error) {
	return r.base.logs(ctx, project, options)
}

func (r *PHPRuntime) HealthCheck(ctx context.Context, project ProjectContext) (HealthResult, error) {
	status, err := r.base.status(ctx, project)
	if err != nil {
		return HealthResult{}, err
	}
	return HealthResult{
		Healthy:   status.State == "running",
		Message:   "PHP-FPM process state: " + status.State,
		CheckedAt: time.Now().UTC(),
	}, nil
}

func composerInvocation(project ProjectContext) (string, []string, error) {
	composer, err := findExecutable("composer")
	if err != nil {
		return "", nil, err
	}
	php, err := projectExecutable(project, "php", "php")
	if err != nil {
		return "", nil, err
	}
	return php, []string{composer}, nil
}

func loadComposerManifest(workDir string) (*composerManifest, error) {
	content, err := readProjectFile(workDir, "composer.json")
	if err != nil || content == nil {
		return nil, err
	}
	var manifest composerManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("parse composer.json: %w", err)
	}
	if manifest.Require == nil {
		manifest.Require = make(map[string]string)
	}
	return &manifest, nil
}

func composerRequires(manifest *composerManifest, packageName string) bool {
	if manifest == nil {
		return false
	}
	_, ok := manifest.Require[packageName]
	return ok
}

func composerExtensions(manifest *composerManifest) []string {
	if manifest == nil {
		return nil
	}
	result := make([]string, 0)
	for name := range manifest.Require {
		if strings.HasPrefix(strings.ToLower(name), "ext-") {
			result = append(result, strings.ToLower(name))
		}
	}
	sort.Strings(result)
	return result
}

func phpExtensions(ctx context.Context, php string) (map[string]bool, error) {
	output, err := exec.CommandContext(ctx, php, "-m").Output()
	if err != nil {
		return nil, fmt.Errorf("query PHP extensions: %w", err)
	}
	result := make(map[string]bool)
	for _, line := range strings.Split(strings.ToLower(string(output)), "\n") {
		if name := strings.TrimSpace(line); name != "" && !strings.HasPrefix(name, "[") {
			result[name] = true
		}
	}
	return result, nil
}

func phpDocumentRoot(workDir, framework string) string {
	if (framework == "Laravel" || framework == "Symfony") && fileExists(workDir, filepath.Join("public", "index.php")) {
		return "public"
	}
	return "."
}

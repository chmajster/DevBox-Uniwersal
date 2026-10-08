package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type composerManifest struct {
	Require map[string]string `json:"require"`
}
type PHPRuntime struct{}

func NewPHPRuntime() *PHPRuntime   { return &PHPRuntime{} }
func (r *PHPRuntime) Name() string { return "php" }

func (r *PHPRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}

	files := existingFiles(project.WorkDir, "artisan", "composer.json", "index.php", "symfony.lock")
	phpEntries, globErr := filepath.Glob(filepath.Join(project.WorkDir, "*.php"))
	if globErr != nil {
		return Detection{}, globErr
	}
	if len(phpEntries) > 0 {
		name := filepath.Base(phpEntries[0])
		found := false
		for _, file := range files {
			found = found || file == name
		}
		if !found {
			files = append(files, name)
		}
	}
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
		"php -S 0.0.0.0:8080 -t "+phpDocumentRoot(project.WorkDir, framework),
		map[string]any{
			"required_extensions": extensions,
			"document_root":       phpDocumentRoot(project.WorkDir, framework),
		},
	)
	detection.Version = version
	return detection, nil
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

func phpDocumentRoot(workDir, framework string) string {
	if (framework == "Laravel" || framework == "Symfony") && fileExists(workDir, filepath.Join("public", "index.php")) {
		return "public"
	}
	return "."
}

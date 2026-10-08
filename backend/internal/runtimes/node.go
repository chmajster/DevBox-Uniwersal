package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type nodeManifest struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"`
	Main            string            `json:"main"`
}
type NodeRuntime struct{}

func NewNodeRuntime() *NodeRuntime  { return &NodeRuntime{} }
func (r *NodeRuntime) Name() string { return "node" }

func (r *NodeRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}
	manifest, err := loadNodeManifest(project.WorkDir)
	if err != nil {
		return Detection{}, err
	}
	if manifest == nil {
		return Detection{Runtime: r.Name()}, nil
	}

	viteConfig := firstExistingFile(project.WorkDir,
		"vite.config.js",
		"vite.config.ts",
		"vite.config.mjs",
		"vite.config.mts",
		"vite.config.cjs",
		"vite.config.cts",
	)
	files := existingFiles(project.WorkDir, "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock")
	if viteConfig != "" {
		files = append(files, viteConfig)
	}
	manager := nodePackageManager(project.WorkDir, manifest)
	framework := "Node.js"
	confidence := 88
	switch {
	case viteConfig != "" || nodeHasPackage(manifest, "vite"):
		framework = "Vite"
		confidence = 96
	case nodeHasPackage(manifest, "next"):
		framework = "Next.js"
		confidence = 95
	case nodeHasPackage(manifest, "express"):
		framework = "Express"
		confidence = 92
	}

	buildCommand := ""
	if manifest.Scripts["build"] != "" {
		buildCommand = managerRunCommand(manager, "build")
	}
	startCommand := ""
	switch {
	case manifest.Scripts["start"] != "":
		startCommand = managerRunCommand(manager, "start")
	case framework == "Vite" && manifest.Scripts["dev"] != "":
		startCommand = managerRunCommand(manager, "dev") + " -- --host 127.0.0.1 --port $PORT"
	case strings.TrimSpace(manifest.Main) != "":
		startCommand = "node " + manifest.Main
	}

	return newDetection(
		r.Name(),
		framework,
		confidence,
		files,
		buildCommand,
		startCommand,
		map[string]any{
			"package_manager":           manager,
			"suggested_install_command": nodeInstallCommand(manager, fileExists(project.WorkDir, managerLockfile(manager))),
		},
	), nil
}

func loadNodeManifest(workDir string) (*nodeManifest, error) {
	content, err := readProjectFile(workDir, "package.json")
	if err != nil || content == nil {
		return nil, err
	}
	var manifest nodeManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("parse package.json: %w", err)
	}
	if manifest.Scripts == nil {
		manifest.Scripts = make(map[string]string)
	}
	return &manifest, nil
}

func nodeHasPackage(manifest *nodeManifest, packageName string) bool {
	if manifest == nil {
		return false
	}
	if _, ok := manifest.Dependencies[packageName]; ok {
		return true
	}
	_, ok := manifest.DevDependencies[packageName]
	return ok
}

func nodePackageManager(workDir string, manifest *nodeManifest) string {
	switch {
	case fileExists(workDir, "pnpm-lock.yaml"):
		return "pnpm"
	case fileExists(workDir, "yarn.lock"):
		return "yarn"
	case fileExists(workDir, "package-lock.json"):
		return "npm"
	case manifest != nil && strings.HasPrefix(strings.ToLower(manifest.PackageManager), "pnpm@"):
		return "pnpm"
	case manifest != nil && strings.HasPrefix(strings.ToLower(manifest.PackageManager), "yarn@"):
		return "yarn"
	default:
		return "npm"
	}
}

func managerLockfile(manager string) string {
	switch manager {
	case "pnpm":
		return "pnpm-lock.yaml"
	case "yarn":
		return "yarn.lock"
	default:
		return "package-lock.json"
	}
}

func nodeInstallArgs(manager string, locked bool) []string {
	switch manager {
	case "pnpm":
		if locked {
			return []string{"install", "--frozen-lockfile"}
		}
		return []string{"install"}
	case "yarn":
		if locked {
			return []string{"install", "--frozen-lockfile"}
		}
		return []string{"install"}
	default:
		if locked {
			return []string{"ci"}
		}
		return []string{"install"}
	}
}

func nodeInstallCommand(manager string, locked bool) string {
	return strings.Join(append([]string{manager}, nodeInstallArgs(manager, locked)...), " ")
}

func managerRunCommand(manager, script string) string {
	return manager + " run " + script
}

package runtimes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type nodeManifest struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"`
	Engines         map[string]string `json:"engines"`
	Main            string            `json:"main"`
}

type NodeRuntime struct {
	base runtimeBase
}

func NewNodeRuntime(processes providers.ProcessManager, runner CommandRunner) *NodeRuntime {
	return &NodeRuntime{base: newRuntimeBase(processes, runner)}
}

func (r *NodeRuntime) Name() string { return "node" }

func (r *NodeRuntime) Inspect(ctx context.Context) RuntimeInfo {
	node := inspectExecutable(ctx, "node", []string{"node"}, "--version")
	npm := inspectExecutable(ctx, "npm", []string{"npm"}, "--version")
	pnpm := inspectExecutable(ctx, "pnpm", []string{"pnpm"}, "--version")
	yarn := inspectExecutable(ctx, "yarn", []string{"yarn"}, "--version")
	return aggregateRuntimeInfo(r.Name(), node, nil, []DependencyInfo{npm, pnpm, yarn})
}

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
	files := existingFiles(project.WorkDir, "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", ".nvmrc", ".node-version")
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

	detection := newDetection(
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
	)
	detection.Version = nodeVersionRequirement(project.WorkDir, manifest)
	return detection, nil
}

func (r *NodeRuntime) Validate(_ context.Context, project ProjectContext) (ValidationResult, error) {
	result := ValidationResult{Valid: true}
	if err := validateWorkDir(project); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	manifest, err := loadNodeManifest(project.WorkDir)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if manifest == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "package.json was not found")
		return result, nil
	}
	if _, err := projectExecutable(project, "node", "node"); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	}
	lockfiles := existingFiles(project.WorkDir, "package-lock.json", "pnpm-lock.yaml", "yarn.lock")
	if len(lockfiles) > 1 {
		result.Valid = false
		result.Errors = append(result.Errors, "multiple Node.js lockfiles detected: "+strings.Join(lockfiles, ", "))
	}
	manager := nodePackageManager(project.WorkDir, manifest)
	if _, _, err := nodeManagerInvocation(project, manager); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("%s is required by the selected lockfile/packageManager: %v", manager, err))
	}
	if _, ok := projectPort(project); !ok {
		result.Warnings = append(result.Warnings, "runtime port is not configured; PORT will not be injected")
	}
	if manifest.Scripts["start"] == "" && manifest.Main == "" && !(projectMode(project) == "development" && manifest.Scripts["dev"] != "") {
		result.Warnings = append(result.Warnings, "no start script or package main entrypoint was found")
	}
	return result, nil
}

func (r *NodeRuntime) InstallDependencies(ctx context.Context, project ProjectContext) error {
	manifest, err := loadNodeManifest(project.WorkDir)
	if err != nil {
		return err
	}
	if manifest == nil {
		return fmt.Errorf("package.json was not found")
	}
	manager := nodePackageManager(project.WorkDir, manifest)
	executable, prefix, err := nodeManagerInvocation(project, manager)
	if err != nil {
		return err
	}
	args := append(prefix, nodeInstallArgs(manager, fileExists(project.WorkDir, managerLockfile(manager)))...)
	return r.base.runner.Run(ctx, executable, args, project.WorkDir, projectEnvironmentFor(project, "node"))
}

func (r *NodeRuntime) Build(ctx context.Context, project ProjectContext) error {
	manifest, err := loadNodeManifest(project.WorkDir)
	if err != nil {
		return err
	}
	if manifest == nil || manifest.Scripts["build"] == "" {
		return nil
	}
	manager := nodePackageManager(project.WorkDir, manifest)
	executable, prefix, err := nodeManagerInvocation(project, manager)
	if err != nil {
		return err
	}
	args := append(prefix, "run", "build")
	return r.base.runner.Run(ctx, executable, args, project.WorkDir, projectEnvironmentFor(project, "node"))
}

func (r *NodeRuntime) Start(_ context.Context, project ProjectContext) error {
	manifest, err := loadNodeManifest(project.WorkDir)
	if err != nil {
		return err
	}
	if manifest == nil {
		return fmt.Errorf("package.json was not found")
	}
	manager := nodePackageManager(project.WorkDir, manifest)
	environment := projectEnvironmentFor(project, "node")
	port, hasPort := projectPort(project)
	if hasPort {
		environment["PORT"] = fmt.Sprintf("%d", port)
	}

	if manifest.Scripts["start"] != "" {
		executable, prefix, err := nodeManagerInvocation(project, manager)
		if err != nil {
			return err
		}
		return r.base.start(project, executable, append(prefix, "run", "start"), environment)
	}
	if projectMode(project) == "development" && manifest.Scripts["dev"] != "" {
		if !hasPort {
			return fmt.Errorf("runtime port is not configured")
		}
		executable, prefix, err := nodeManagerInvocation(project, manager)
		if err != nil {
			return err
		}
		return r.base.start(project, executable, append(prefix, "run", "dev", "--", "--host", "127.0.0.1", "--port", fmt.Sprintf("%d", port)), environment)
	}
	if strings.TrimSpace(manifest.Main) != "" {
		node, err := projectExecutable(project, "node", "node")
		if err != nil {
			return err
		}
		return r.base.start(project, node, []string{manifest.Main}, environment)
	}
	return fmt.Errorf("Node.js project does not define a runnable start script or main entrypoint")
}

func (r *NodeRuntime) Stop(ctx context.Context, project ProjectContext) error {
	return r.base.stop(ctx, project)
}

func (r *NodeRuntime) Restart(ctx context.Context, project ProjectContext) error {
	return r.base.restart(ctx, project)
}

func (r *NodeRuntime) Status(ctx context.Context, project ProjectContext) (ProcessStatus, error) {
	return r.base.status(ctx, project)
}

func (r *NodeRuntime) Logs(ctx context.Context, project ProjectContext, options LogOptions) (io.ReadCloser, error) {
	return r.base.logs(ctx, project, options)
}

func (r *NodeRuntime) HealthCheck(ctx context.Context, project ProjectContext) (HealthResult, error) {
	return r.base.httpHealth(ctx, project)
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


func nodeVersionRequirement(workDir string, manifest *nodeManifest) string {
	for _, filename := range []string{".nvmrc", ".node-version"} {
		content, err := readProjectFile(workDir, filename)
		if err == nil && content != nil {
			value := strings.TrimSpace(string(content))
			value = strings.TrimPrefix(value, "v")
			if value != "" {
				return value
			}
		}
	}
	if manifest != nil {
		return strings.TrimSpace(manifest.Engines["node"])
	}
	return ""
}

func nodeManagerInvocation(project ProjectContext, manager string) (string, []string, error) {
	if manager == "npm" {
		executable, err := projectExecutable(project, "npm", "npm")
		return executable, nil, err
	}
	if project.Executables != nil && strings.TrimSpace(project.Executables["node"]) != "" {
		if direct := strings.TrimSpace(project.Executables[manager]); direct != "" {
			executable, err := projectExecutable(project, manager, manager)
			return executable, nil, err
		}
		if strings.TrimSpace(project.Executables["corepack"]) != "" {
			corepack, err := projectExecutable(project, "corepack", "corepack")
			if err != nil {
				return "", nil, err
			}
			return corepack, []string{manager}, nil
		}
		return "", nil, fmt.Errorf("assigned Node runtime does not provide %s or corepack", manager)
	}
	executable, err := findExecutable(manager)
	return executable, nil, err
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

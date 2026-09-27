package runtimes

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type GoRuntime struct {
	base runtimeBase
}

func NewGoRuntime(processes providers.ProcessManager, runner CommandRunner) *GoRuntime {
	return &GoRuntime{base: newRuntimeBase(processes, runner)}
}

func (r *GoRuntime) Name() string { return "go" }

func (r *GoRuntime) Inspect(ctx context.Context) RuntimeInfo {
	goTool := inspectExecutable(ctx, "go", []string{"go"}, "version")
	return aggregateRuntimeInfo(r.Name(), goTool, nil, nil)
}

func (r *GoRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}
	content, err := readProjectFile(project.WorkDir, "go.mod")
	if err != nil {
		return Detection{}, err
	}
	if content == nil {
		return Detection{Runtime: r.Name()}, nil
	}
	version := ""
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			version = fields[1]
			break
		}
	}
	detection := newDetection(
		r.Name(),
		"Go",
		96,
		[]string{"go.mod"},
		"go mod download && go build -o .devbox/build/app .",
		".devbox/build/app",
		map[string]any{"binary": filepath.ToSlash(filepath.Join(".devbox", "build", goBinaryName()))},
	)
	detection.Version = version
	return detection, nil
}

func (r *GoRuntime) Validate(_ context.Context, project ProjectContext) (ValidationResult, error) {
	result := ValidationResult{Valid: true}
	if err := validateWorkDir(project); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if !fileExists(project.WorkDir, "go.mod") {
		result.Valid = false
		result.Errors = append(result.Errors, "go.mod was not found")
	}
	if _, err := findExecutable("go"); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	}
	if _, ok := projectPort(project); !ok {
		result.Warnings = append(result.Warnings, "runtime port is not configured; PORT will not be injected")
	}
	return result, nil
}

func (r *GoRuntime) InstallDependencies(ctx context.Context, project ProjectContext) error {
	goTool, err := findExecutable("go")
	if err != nil {
		return err
	}
	return r.base.runner.Run(ctx, goTool, []string{"mod", "download"}, project.WorkDir, project.Environment)
}

func (r *GoRuntime) Build(ctx context.Context, project ProjectContext) error {
	goTool, err := findExecutable("go")
	if err != nil {
		return err
	}
	buildDir := filepath.Join(project.WorkDir, ".devbox", "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("create Go build directory: %w", err)
	}
	target := "."
	if configured, ok := project.Config["package"].(string); ok && strings.TrimSpace(configured) != "" {
		target = configured
	}
	return r.base.runner.Run(ctx, goTool, []string{"build", "-o", goBinaryPath(project.WorkDir), target}, project.WorkDir, project.Environment)
}

func (r *GoRuntime) Start(_ context.Context, project ProjectContext) error {
	binary := goBinaryPath(project.WorkDir)
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("Go runtime binary is missing; Build must complete first")
	}
	environment := map[string]string{}
	if port, ok := projectPort(project); ok {
		environment["PORT"] = fmt.Sprintf("%d", port)
	}
	return r.base.start(project, binary, nil, environment)
}

func (r *GoRuntime) Stop(ctx context.Context, project ProjectContext) error {
	return r.base.stop(ctx, project)
}

func (r *GoRuntime) Restart(ctx context.Context, project ProjectContext) error {
	return r.base.restart(ctx, project)
}

func (r *GoRuntime) Status(ctx context.Context, project ProjectContext) (ProcessStatus, error) {
	return r.base.status(ctx, project)
}

func (r *GoRuntime) Logs(ctx context.Context, project ProjectContext, options LogOptions) (io.ReadCloser, error) {
	return r.base.logs(ctx, project, options)
}

func (r *GoRuntime) HealthCheck(ctx context.Context, project ProjectContext) (HealthResult, error) {
	return r.base.httpHealth(ctx, project)
}

func goBinaryName() string {
	if goruntime.GOOS == "windows" {
		return "app.exe"
	}
	return "app"
}

func goBinaryPath(workDir string) string {
	return filepath.Join(workDir, ".devbox", "build", goBinaryName())
}

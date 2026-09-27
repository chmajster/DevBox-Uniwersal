package runtimes

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"regexp"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

var djangoSettingsPattern = regexp.MustCompile(`DJANGO_SETTINGS_MODULE[^"'\n]*["']([^"']+)["']`)

type PythonRuntime struct {
	base runtimeBase
}

func NewPythonRuntime(processes providers.ProcessManager, runner CommandRunner) *PythonRuntime {
	return &PythonRuntime{base: newRuntimeBase(processes, runner)}
}

func (r *PythonRuntime) Name() string { return "python" }

func (r *PythonRuntime) Inspect(ctx context.Context) RuntimeInfo {
	python := inspectExecutable(ctx, "python", []string{"python3", "python"}, "--version")
	pip := inspectExecutable(ctx, "pip", []string{"pip3", "pip"}, "--version")
	uv := inspectExecutable(ctx, "uv", []string{"uv"}, "--version")
	poetry := inspectExecutable(ctx, "poetry", []string{"poetry"}, "--version")
	return aggregateRuntimeInfo(r.Name(), python, nil, []DependencyInfo{pip, uv, poetry})
}

func (r *PythonRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}
	files := existingFiles(project.WorkDir, "manage.py", "pyproject.toml", "requirements.txt", "poetry.lock", "uv.lock")
	if !fileExists(project.WorkDir, "manage.py") && !fileExists(project.WorkDir, "pyproject.toml") && !fileExists(project.WorkDir, "requirements.txt") {
		return Detection{Runtime: r.Name()}, nil
	}

	requirements, err := readProjectFile(project.WorkDir, "requirements.txt")
	if err != nil {
		return Detection{}, err
	}
	pyproject, err := readProjectFile(project.WorkDir, "pyproject.toml")
	if err != nil {
		return Detection{}, err
	}
	dependencyText := strings.ToLower(string(requirements) + "\n" + string(pyproject))
	framework := "Python"
	confidence := 82
	startCommand := "python main.py"
	metadata := map[string]any{"package_manager": pythonPackageManager(project.WorkDir, string(pyproject))}

	switch {
	case fileExists(project.WorkDir, "manage.py") || strings.Contains(dependencyText, "django"):
		framework = "Django"
		confidence = 100
		module := djangoModule(project.WorkDir)
		if module == "" {
			module = "project"
		}
		startCommand = "gunicorn --bind 127.0.0.1:$PORT " + module + ".wsgi:application"
		metadata["application_target"] = module + ".wsgi:application"
		metadata["development_start_command"] = "python manage.py runserver 127.0.0.1:$PORT"
	case strings.Contains(dependencyText, "fastapi"):
		framework = "FastAPI"
		confidence = 96
		target := pythonApplicationTarget(project.WorkDir, "FastAPI")
		startCommand = "uvicorn " + target + " --host 127.0.0.1 --port $PORT"
		metadata["application_target"] = target
	case strings.Contains(dependencyText, "flask"):
		framework = "Flask"
		confidence = 94
		target := pythonApplicationTarget(project.WorkDir, "Flask")
		startCommand = "gunicorn --bind 127.0.0.1:$PORT " + target
		metadata["application_target"] = target
		metadata["development_start_command"] = "flask --app " + strings.Split(target, ":")[0] + " run --host 127.0.0.1 --port $PORT"
	default:
		if fileExists(project.WorkDir, "main.py") {
			startCommand = "python main.py"
			metadata["entrypoint"] = "main.py"
		}
	}

	return newDetection(
		r.Name(),
		framework,
		confidence,
		files,
		"python -m compileall .",
		startCommand,
		metadata,
	), nil
}

func (r *PythonRuntime) Validate(_ context.Context, project ProjectContext) (ValidationResult, error) {
	result := ValidationResult{Valid: true}
	if err := validateWorkDir(project); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if _, err := findExecutable("python3", "python"); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	pyproject, err := readProjectFile(project.WorkDir, "pyproject.toml")
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	manager := pythonPackageManager(project.WorkDir, string(pyproject))
	switch manager {
	case "uv":
		if _, err := findExecutable("uv"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, "uv is selected by the project but is not available")
		}
	case "poetry":
		if _, err := findExecutable("poetry"); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, "Poetry is selected by the project but is not available")
		}
	}
	if !fileExists(project.WorkDir, filepath.Join(".venv", venvPythonRelative())) {
		result.Warnings = append(result.Warnings, "project virtualenv .venv does not exist; install dependencies before Start")
	}
	if _, ok := projectPort(project); !ok {
		result.Warnings = append(result.Warnings, "runtime port is not configured")
	}

	detection, detectErr := r.Detect(context.Background(), project)
	if detectErr == nil && projectMode(project) == "production" && (detectionString(detection, "framework") == "Django" || detectionString(detection, "framework") == "Flask") && goruntime.GOOS == "windows" {
		result.Valid = false
		result.Errors = append(result.Errors, "Gunicorn is not supported on native Windows; use WSL or development mode")
	}
	return result, nil
}

func (r *PythonRuntime) InstallDependencies(ctx context.Context, project ProjectContext) error {
	if err := validateWorkDir(project); err != nil {
		return err
	}
	pyproject, err := readProjectFile(project.WorkDir, "pyproject.toml")
	if err != nil {
		return err
	}
	manager := pythonPackageManager(project.WorkDir, string(pyproject))
	venvDir := filepath.Join(project.WorkDir, ".venv")

	switch manager {
	case "uv":
		uv, err := findExecutable("uv")
		if err != nil {
			return err
		}
		if !fileExists(project.WorkDir, filepath.Join(".venv", venvPythonRelative())) {
			if err := r.base.runner.Run(ctx, uv, []string{"venv", venvDir}, project.WorkDir, project.Environment); err != nil {
				return err
			}
		}
		environment := copyStringMap(project.Environment)
		environment["UV_PROJECT_ENVIRONMENT"] = venvDir
		if fileExists(project.WorkDir, "pyproject.toml") {
			return r.base.runner.Run(ctx, uv, []string{"sync"}, project.WorkDir, environment)
		}
		return r.base.runner.Run(ctx, uv, []string{"pip", "install", "--python", venvPython(project.WorkDir), "-r", "requirements.txt"}, project.WorkDir, environment)
	case "poetry":
		poetry, err := findExecutable("poetry")
		if err != nil {
			return err
		}
		environment := copyStringMap(project.Environment)
		environment["POETRY_VIRTUALENVS_IN_PROJECT"] = "true"
		return r.base.runner.Run(ctx, poetry, []string{"install", "--no-interaction"}, project.WorkDir, environment)
	default:
		python, err := findExecutable("python3", "python")
		if err != nil {
			return err
		}
		if !fileExists(project.WorkDir, filepath.Join(".venv", venvPythonRelative())) {
			if err := r.base.runner.Run(ctx, python, []string{"-m", "venv", venvDir}, project.WorkDir, project.Environment); err != nil {
				return err
			}
		}
		venv := venvPython(project.WorkDir)
		if fileExists(project.WorkDir, "requirements.txt") {
			if err := r.base.runner.Run(ctx, venv, []string{"-m", "pip", "install", "-r", "requirements.txt"}, project.WorkDir, project.Environment); err != nil {
				return err
			}
		}
		if fileExists(project.WorkDir, "pyproject.toml") {
			return r.base.runner.Run(ctx, venv, []string{"-m", "pip", "install", "."}, project.WorkDir, project.Environment)
		}
		return nil
	}
}

func (r *PythonRuntime) Build(ctx context.Context, project ProjectContext) error {
	python := venvPython(project.WorkDir)
	if _, err := os.Stat(python); err != nil {
		return fmt.Errorf("project virtualenv is missing; install dependencies first")
	}
	return r.base.runner.Run(ctx, python, []string{"-m", "compileall", "-q", "."}, project.WorkDir, project.Environment)
}

func (r *PythonRuntime) Start(_ context.Context, project ProjectContext) error {
	python := venvPython(project.WorkDir)
	if _, err := os.Stat(python); err != nil {
		return fmt.Errorf("project virtualenv is missing; install dependencies first")
	}
	detection, err := r.Detect(context.Background(), project)
	if err != nil {
		return err
	}
	framework := detectionString(detection, "framework")
	target := detectionString(detection, "application_target")
	port, hasPort := projectPort(project)
	portValue := fmt.Sprintf("%d", port)
	environment := map[string]string{}
	if hasPort {
		environment["PORT"] = portValue
	}

	var args []string
	switch framework {
	case "FastAPI":
		if !hasPort {
			return fmt.Errorf("runtime port is not configured")
		}
		args = []string{"-m", "uvicorn", target, "--host", "127.0.0.1", "--port", portValue}
	case "Flask":
		if !hasPort {
			return fmt.Errorf("runtime port is not configured")
		}
		if projectMode(project) == "development" {
			module := strings.Split(target, ":")[0]
			args = []string{"-m", "flask", "--app", module, "run", "--host", "127.0.0.1", "--port", portValue}
		} else {
			args = []string{"-m", "gunicorn", "--bind", "127.0.0.1:" + portValue, target}
		}
	case "Django":
		if !hasPort {
			return fmt.Errorf("runtime port is not configured")
		}
		if projectMode(project) == "development" {
			args = []string{"manage.py", "runserver", "127.0.0.1:" + portValue}
		} else {
			args = []string{"-m", "gunicorn", "--bind", "127.0.0.1:" + portValue, target}
		}
	default:
		entrypoint := detectionString(detection, "entrypoint")
		if configured, ok := project.Config["entrypoint"].(string); ok && strings.TrimSpace(configured) != "" {
			entrypoint = configured
		}
		if entrypoint == "" {
			return fmt.Errorf("generic Python project requires an entrypoint")
		}
		args = []string{entrypoint}
	}
	return r.base.start(project, python, args, environment)
}

func (r *PythonRuntime) Stop(ctx context.Context, project ProjectContext) error {
	return r.base.stop(ctx, project)
}

func (r *PythonRuntime) Restart(ctx context.Context, project ProjectContext) error {
	return r.base.restart(ctx, project)
}

func (r *PythonRuntime) Status(ctx context.Context, project ProjectContext) (ProcessStatus, error) {
	return r.base.status(ctx, project)
}

func (r *PythonRuntime) Logs(ctx context.Context, project ProjectContext, options LogOptions) (io.ReadCloser, error) {
	return r.base.logs(ctx, project, options)
}

func (r *PythonRuntime) HealthCheck(ctx context.Context, project ProjectContext) (HealthResult, error) {
	return r.base.httpHealth(ctx, project)
}

func pythonPackageManager(workDir, pyproject string) string {
	switch {
	case fileExists(workDir, "uv.lock"):
		return "uv"
	case fileExists(workDir, "poetry.lock") || strings.Contains(strings.ToLower(pyproject), "[tool.poetry]"):
		return "poetry"
	default:
		return "pip"
	}
}

func venvPythonRelative() string {
	if goruntime.GOOS == "windows" {
		return filepath.Join("Scripts", "python.exe")
	}
	return filepath.Join("bin", "python")
}

func venvPython(workDir string) string {
	return filepath.Join(workDir, ".venv", venvPythonRelative())
}

func pythonApplicationTarget(workDir, framework string) string {
	for _, filename := range []string{"main.py", "app.py"} {
		content, err := readProjectFile(workDir, filename)
		if err != nil || content == nil {
			continue
		}
		text := strings.ToLower(string(content))
		if (framework == "FastAPI" && strings.Contains(text, "fastapi(")) || (framework == "Flask" && strings.Contains(text, "flask(")) {
			return strings.TrimSuffix(filename, ".py") + ":app"
		}
	}
	if framework == "Flask" {
		return "app:app"
	}
	return "main:app"
}

func djangoModule(workDir string) string {
	content, err := readProjectFile(workDir, "manage.py")
	if err != nil || content == nil {
		return ""
	}
	match := djangoSettingsPattern.FindSubmatch(content)
	if len(match) != 2 {
		return ""
	}
	module := string(match[1])
	return strings.TrimSuffix(module, ".settings")
}

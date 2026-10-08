package runtimes

import (
	"context"
	"regexp"
	"strings"
)

var djangoSettingsPattern = regexp.MustCompile(`DJANGO_SETTINGS_MODULE[^"'\n]*["']([^"']+)["']`)

type PythonRuntime struct{}

func NewPythonRuntime() *PythonRuntime { return &PythonRuntime{} }
func (r *PythonRuntime) Name() string  { return "python" }

func (r *PythonRuntime) Detect(_ context.Context, project ProjectContext) (Detection, error) {
	if err := validateWorkDir(project); err != nil {
		return Detection{}, err
	}
	files := existingFiles(project.WorkDir, "manage.py", "pyproject.toml", "requirements.txt", "poetry.lock", "uv.lock", "main.py", "app.py")
	if !fileExists(project.WorkDir, "manage.py") && !fileExists(project.WorkDir, "pyproject.toml") && !fileExists(project.WorkDir, "requirements.txt") && !fileExists(project.WorkDir, "main.py") && !fileExists(project.WorkDir, "app.py") {
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

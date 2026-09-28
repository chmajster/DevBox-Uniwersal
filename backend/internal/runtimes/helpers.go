package runtimes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type CommandRunner interface {
	Run(ctx context.Context, command string, args []string, workDir string, environment map[string]string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command string, args []string, workDir string, environment map[string]string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("command is empty")
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = workDir
	cmd.Env = mergedEnvironment(environment)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", filepath.Base(command), err)
	}
	return nil
}

type runtimeBase struct {
	processes providers.ProcessManager
	runner    CommandRunner
}

func newRuntimeBase(processes providers.ProcessManager, runner CommandRunner) runtimeBase {
	return runtimeBase{processes: processes, runner: runner}
}

func (b runtimeBase) start(project ProjectContext, command string, args []string, extraEnvironment map[string]string) error {
	if b.processes == nil {
		return fmt.Errorf("process manager is not configured")
	}
	environment := copyStringMap(project.Environment)
	for key, value := range extraEnvironment {
		environment[key] = value
	}
	_, err := b.processes.Start(context.Background(), providers.ProcessSpec{
		Name:        processName(project),
		Command:     command,
		Args:        append([]string(nil), args...),
		WorkDir:     project.WorkDir,
		Environment: environment,
	})
	return err
}

func (b runtimeBase) stop(ctx context.Context, project ProjectContext) error {
	if b.processes == nil {
		return fmt.Errorf("process manager is not configured")
	}
	return b.processes.Stop(ctx, processName(project))
}

func (b runtimeBase) restart(ctx context.Context, project ProjectContext) error {
	if b.processes == nil {
		return fmt.Errorf("process manager is not configured")
	}
	return b.processes.Restart(ctx, processName(project))
}

func (b runtimeBase) status(ctx context.Context, project ProjectContext) (ProcessStatus, error) {
	if b.processes == nil {
		return ProcessStatus{}, fmt.Errorf("process manager is not configured")
	}
	info, err := b.processes.Status(ctx, processName(project))
	if err != nil {
		return ProcessStatus{}, err
	}
	return ProcessStatus{State: info.State, PID: info.PID, StartedAt: info.StartedAt}, nil
}

func (b runtimeBase) logs(ctx context.Context, project ProjectContext, options LogOptions) (io.ReadCloser, error) {
	if b.processes == nil {
		return nil, fmt.Errorf("process manager is not configured")
	}
	return b.processes.Logs(ctx, processName(project), options.Tail, options.Follow)
}

func (b runtimeBase) httpHealth(ctx context.Context, project ProjectContext) (HealthResult, error) {
	checkedAt := time.Now().UTC()
	port, ok := projectPort(project)
	if !ok {
		status, err := b.status(ctx, project)
		if err != nil {
			return HealthResult{}, err
		}
		return HealthResult{
			Healthy:   status.State == "running",
			Message:   "no HTTP port configured; process state used",
			CheckedAt: checkedAt,
		}, nil
	}

	started := time.Now()
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	if err != nil {
		return HealthResult{}, err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return HealthResult{Healthy: false, Message: err.Error(), Latency: time.Since(started), CheckedAt: checkedAt}, nil
	}
	defer response.Body.Close()
	healthy := response.StatusCode >= 200 && response.StatusCode < 500
	return HealthResult{
		Healthy:   healthy,
		Message:   response.Status,
		Latency:   time.Since(started),
		CheckedAt: checkedAt,
	}, nil
}

func newDetection(runtimeName, framework string, confidence int, files []string, buildCommand, startCommand string, metadata map[string]any) Detection {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["framework"] = framework
	metadata["confidence"] = confidence
	metadata["detected_files"] = append([]string(nil), files...)
	metadata["suggested_build_command"] = buildCommand
	metadata["suggested_start_command"] = startCommand
	return Detection{Detected: true, Runtime: runtimeName, Metadata: metadata}
}

func detectionConfidence(detection Detection) int {
	value, ok := detection.Metadata["confidence"]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func detectionString(detection Detection, key string) string {
	value, _ := detection.Metadata[key].(string)
	return value
}

func detectionStrings(detection Detection, key string) []string {
	value, ok := detection.Metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func fileExists(root, name string) bool {
	info, err := os.Stat(filepath.Join(root, name))
	return err == nil && !info.IsDir()
}

func readProjectFile(root, name string) ([]byte, error) {
	content, err := os.ReadFile(filepath.Join(root, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return content, nil
}

func existingFiles(root string, names ...string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		if fileExists(root, name) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}

func firstExistingFile(root string, names ...string) string {
	for _, name := range names {
		if fileExists(root, name) {
			return name
		}
	}
	return ""
}

func validateWorkDir(project ProjectContext) error {
	if strings.TrimSpace(project.WorkDir) == "" {
		return fmt.Errorf("project work directory is not configured")
	}
	info, err := os.Stat(project.WorkDir)
	if err != nil {
		return fmt.Errorf("project work directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project work directory is not a directory")
	}
	return nil
}

func projectPort(project ProjectContext) (int, bool) {
	if project.Config != nil {
		if value, exists := project.Config["port"]; exists {
			switch typed := value.(type) {
			case int:
				if typed > 0 && typed <= 65535 {
					return typed, true
				}
			case int64:
				if typed > 0 && typed <= 65535 {
					return int(typed), true
				}
			case float64:
				if typed > 0 && typed <= 65535 {
					return int(typed), true
				}
			case string:
				if parsed, err := strconv.Atoi(typed); err == nil && parsed > 0 && parsed <= 65535 {
					return parsed, true
				}
			}
		}
	}
	if value := strings.TrimSpace(project.Environment["PORT"]); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 65535 {
			return parsed, true
		}
	}
	return 0, false
}

func projectMode(project ProjectContext) string {
	if project.Config != nil {
		if value, ok := project.Config["mode"].(string); ok {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "development" || value == "production" {
				return value
			}
		}
	}
	return "production"
}

func processName(project ProjectContext) string {
	value := strings.TrimSpace(project.ProjectID)
	if value == "" {
		value = strings.TrimSpace(project.ProjectName)
	}
	var buffer bytes.Buffer
	for _, char := range strings.ToLower(value) {
		switch {
		case char >= 'a' && char <= 'z':
			buffer.WriteRune(char)
		case char >= '0' && char <= '9':
			buffer.WriteRune(char)
		default:
			buffer.WriteByte('-')
		}
	}
	name := strings.Trim(buffer.String(), "-")
	if name == "" {
		name = "project"
	}
	return "devbox-runtime-" + name
}

func mergedEnvironment(overrides map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func projectEnvironmentFor(project ProjectContext, runtimeType string) map[string]string {
	environment := copyStringMap(project.Environment)
	if project.Executables == nil {
		return environment
	}
	executable := strings.TrimSpace(project.Executables[runtimeType])
	if executable == "" || !filepath.IsAbs(executable) {
		return environment
	}
	current := environment["PATH"]
	if current == "" {
		current = os.Getenv("PATH")
	}
	environment["PATH"] = filepath.Dir(executable) + string(os.PathListSeparator) + current
	return environment
}

func copyStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func boolMetadata(detection Detection, key string) bool {
	value, _ := detection.Metadata[key].(bool)
	return value
}

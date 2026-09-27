package runtimes

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

const (
	AvailabilityAvailable = "available"
	AvailabilityMissing   = "missing"
	AvailabilityInvalid   = "invalid"
)

type DependencyInfo struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

type RuntimeInfo struct {
	Runtime      string           `json:"runtime"`
	Status       string           `json:"status"`
	Version      string           `json:"version,omitempty"`
	Dependencies []DependencyInfo `json:"dependencies,omitempty"`
}

type Inspector interface {
	Inspect(ctx context.Context) RuntimeInfo
}

type RuntimeRegistry struct {
	mu       sync.RWMutex
	runtimes map[string]Runtime
	order    []string
}

func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{runtimes: make(map[string]Runtime)}
}

func NewDefaultRegistry() *RuntimeRegistry {
	processes := NewLocalProcessManager()
	runner := ExecRunner{}
	registry := NewRuntimeRegistry()
	for _, runtime := range []Runtime{
		NewStaticRuntime(),
		NewPHPRuntime(processes, runner),
		NewPythonRuntime(processes, runner),
		NewGoRuntime(processes, runner),
		NewNodeRuntime(processes, runner),
	} {
		if err := registry.Register(runtime); err != nil {
			panic(err)
		}
	}
	return registry
}

func (r *RuntimeRegistry) Register(runtime Runtime) error {
	if runtime == nil {
		return fmt.Errorf("runtime is nil")
	}
	name := strings.ToLower(strings.TrimSpace(runtime.Name()))
	if name == "" {
		return fmt.Errorf("runtime name is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runtimes[name]; exists {
		return fmt.Errorf("runtime %q is already registered", name)
	}
	r.runtimes[name] = runtime
	r.order = append(r.order, name)
	return nil
}

func (r *RuntimeRegistry) Get(name string) (Runtime, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runtime, ok := r.runtimes[strings.ToLower(strings.TrimSpace(name))]
	return runtime, ok
}

func (r *RuntimeRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := append([]string(nil), r.order...)
	return result
}

func inspectExecutable(ctx context.Context, name string, candidates []string, versionArgs ...string) DependencyInfo {
	info := DependencyInfo{Name: name, Status: AvailabilityMissing}
	var path string
	for _, candidate := range candidates {
		resolved, err := exec.LookPath(candidate)
		if err == nil {
			path = resolved
			break
		}
	}
	if path == "" {
		return info
	}

	info.Path = path
	output, err := exec.CommandContext(ctx, path, versionArgs...).CombinedOutput()
	if err != nil {
		info.Status = AvailabilityInvalid
		return info
	}
	version := firstNonEmptyLine(string(output))
	if version == "" {
		info.Status = AvailabilityInvalid
		return info
	}
	info.Status = AvailabilityAvailable
	info.Version = version
	return info
}

func aggregateRuntimeInfo(name string, primary DependencyInfo, required []DependencyInfo, optional []DependencyInfo) RuntimeInfo {
	status := primary.Status
	if status == AvailabilityAvailable {
		for _, dependency := range required {
			if dependency.Status != AvailabilityAvailable {
				status = AvailabilityInvalid
				break
			}
		}
	}

	dependencies := make([]DependencyInfo, 0, 1+len(required)+len(optional))
	dependencies = append(dependencies, primary)
	dependencies = append(dependencies, required...)
	dependencies = append(dependencies, optional...)
	sort.SliceStable(dependencies, func(i, j int) bool {
		return dependencies[i].Name < dependencies[j].Name
	})

	return RuntimeInfo{
		Runtime:      name,
		Status:       status,
		Version:      primary.Version,
		Dependencies: dependencies,
	}
}

func firstNonEmptyLine(value string) string {
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func findExecutable(candidates ...string) (string, error) {
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("none of the executables are available: %s", strings.Join(candidates, ", "))
}

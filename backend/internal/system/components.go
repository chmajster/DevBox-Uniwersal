package system

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ComponentStatus is the transport-neutral state of a host dependency.
type ComponentStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}

type componentSpec struct {
	name       string
	candidates []string
	args       []string
}

var componentSpecs = []componentSpec{
	{name: "git", candidates: []string{"git"}, args: []string{"--version"}},
	{name: "docker", candidates: []string{"docker"}, args: []string{"--version"}},
	{name: "nginx", candidates: []string{"nginx"}, args: []string{"-v"}},
	{name: "mysql", candidates: []string{"mysql", "mariadb"}, args: []string{"--version"}},
	{name: "php", candidates: []string{"php"}, args: []string{"--version"}},
	{name: "composer", candidates: []string{"composer"}, args: []string{"--version"}},
	{name: "python", candidates: []string{"python3", "python"}, args: []string{"--version"}},
	{name: "pip", candidates: []string{"pip3", "pip"}, args: []string{"--version"}},
	{name: "go", candidates: []string{"go"}, args: []string{"version"}},
	{name: "node", candidates: []string{"node"}, args: []string{"--version"}},
	{name: "npm", candidates: []string{"npm"}, args: []string{"--version"}},
}

// CommandRunner exists so detection can be tested without invoking host binaries.
type CommandRunner interface {
	LookPath(file string) (string, error)
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) LookPath(file string) (string, error) { return exec.LookPath(file) }

func (execRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func DetectComponents(ctx context.Context) []ComponentStatus {
	return DetectComponentsWithRunner(ctx, execRunner{})
}

func DetectComponentsWithRunner(ctx context.Context, runner CommandRunner) []ComponentStatus {
	statuses := make([]ComponentStatus, 0, len(componentSpecs))
	for _, spec := range componentSpecs {
		statuses = append(statuses, detectComponent(ctx, runner, spec))
	}
	return statuses
}

func detectComponent(ctx context.Context, runner CommandRunner, spec componentSpec) ComponentStatus {
	status := ComponentStatus{Name: spec.name, State: "missing"}
	var path string
	var err error
	for _, candidate := range spec.candidates {
		path, err = runner.LookPath(candidate)
		if err == nil {
			break
		}
	}
	if path == "" {
		if err != nil && !errors.Is(err, exec.ErrNotFound) {
			status.Error = err.Error()
		}
		return status
	}

	status.Installed = true
	status.Path = path
	status.State = "available"

	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := runner.CombinedOutput(versionCtx, path, spec.args...)
	if version := ParseVersionOutput(string(out)); version != "" {
		status.Version = version
	}
	if err != nil {
		status.State = "error"
		status.Error = err.Error()
	}
	return status
}

// ParseVersionOutput keeps only the first non-empty line because several tools
// emit verbose multi-line version banners.
func ParseVersionOutput(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

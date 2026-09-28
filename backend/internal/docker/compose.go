package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type composeRunner struct {
	runner commandRunner
	legacy bool
}

func (p *CLIProvider) resolveComposeRunner(ctx context.Context) (composeRunner, error) {
	_, _, pluginErr := p.runner.Run(ctx, "compose", "version")
	if pluginErr == nil {
		return composeRunner{runner: p.runner}, nil
	}
	if p.legacyComposeRunner != nil {
		_, _, legacyErr := p.legacyComposeRunner.Run(ctx, "version")
		if legacyErr == nil {
			return composeRunner{runner: p.legacyComposeRunner, legacy: true}, nil
		}
		return composeRunner{}, fmt.Errorf("%w: Docker Compose is unavailable; install it from Plugins -> Docker Compose or run the DevBox updater/repair; docker compose failed: %v; docker-compose failed: %v", ErrUnavailable, pluginErr, legacyErr)
	}
	return composeRunner{}, fmt.Errorf("%w: Docker Compose plugin is unavailable: %v", ErrUnavailable, pluginErr)
}

func (p *CLIProvider) runCompose(ctx context.Context, args ...string) ([]byte, []byte, error) {
	resolved, err := p.resolveComposeRunner(ctx)
	if err != nil {
		return nil, nil, err
	}
	if resolved.legacy {
		if len(args) == 0 || args[0] != "compose" {
			return nil, nil, fmt.Errorf("%w: invalid compose invocation", ErrInvalidInput)
		}
		return resolved.runner.Run(ctx, args[1:]...)
	}

	stdout, stderr, runErr := resolved.runner.Run(ctx, args...)
	if runErr == nil || !composeInvocationUnsupported(runErr) || p.legacyComposeRunner == nil {
		return stdout, stderr, runErr
	}
	if len(args) == 0 || args[0] != "compose" {
		return stdout, stderr, runErr
	}
	if _, _, legacyErr := p.legacyComposeRunner.Run(ctx, "version"); legacyErr != nil {
		return stdout, stderr, runErr
	}
	legacyStdout, legacyStderr, legacyErr := p.legacyComposeRunner.Run(ctx, args[1:]...)
	if legacyErr != nil {
		return legacyStdout, legacyStderr, fmt.Errorf("docker compose failed: %v; docker-compose fallback failed: %w", runErr, legacyErr)
	}
	return legacyStdout, legacyStderr, nil
}

func (p *CLIProvider) streamCompose(ctx context.Context, args ...string) (io.ReadCloser, error) {
	resolved, err := p.resolveComposeRunner(ctx)
	if err != nil {
		return nil, err
	}
	if resolved.legacy {
		if len(args) == 0 || args[0] != "compose" {
			return nil, fmt.Errorf("%w: invalid compose invocation", ErrInvalidInput)
		}
		args = args[1:]
	}
	return resolved.runner.Stream(ctx, args...)
}

var composeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

func DiscoverComposeProjects(root string) ([]ComposeProject, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(rootAbs)
	if os.IsNotExist(err) {
		return []ComposeProject{}, nil
	}
	if err != nil {
		return nil, err
	}
	projects := make([]ComposeProject, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !containerRefPattern.MatchString(entry.Name()) {
			continue
		}
		dir := filepath.Join(rootAbs, entry.Name())
		configFile, err := findComposeFile(dir)
		if err != nil {
			continue
		}
		projects = append(projects, ComposeProject{Name: entry.Name(), ConfigFile: filepath.Base(configFile)})
	}
	return projects, nil
}

func findComposeFile(dir string) (string, error) {
	dirAbs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for _, name := range composeFileNames {
		path := filepath.Join(dirAbs, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: compose file not found", ErrNotFound)
}

func (p *CLIProvider) ComposeValidate(ctx context.Context, directory, projectName string) error {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	args = append(args, "config", "--quiet")
	_, _, err = p.runCompose(ctx, args...)
	return err
}

func (p *CLIProvider) ComposePull(ctx context.Context, directory, projectName, service string) error {
	return p.composeCommand(ctx, directory, projectName, service, "pull")
}

func (p *CLIProvider) ComposeBuild(ctx context.Context, directory, projectName, service string) error {
	return p.composeCommand(ctx, directory, projectName, service, "build")
}

func (p *CLIProvider) ComposeUp(ctx context.Context, directory, projectName, service string) error {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	if err := validateServiceName(service); err != nil {
		return err
	}
	args = append(args, "up", "--detach")
	if service != "" {
		args = append(args, service)
	}
	_, _, err = p.runCompose(ctx, args...)
	return err
}

func (p *CLIProvider) ComposeDown(ctx context.Context, directory, projectName string) error {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	args = append(args, "down")
	_, _, err = p.runCompose(ctx, args...)
	return err
}

func (p *CLIProvider) ComposeRestart(ctx context.Context, directory, projectName, service string) error {
	return p.composeCommand(ctx, directory, projectName, service, "restart")
}

func (p *CLIProvider) composeCommand(ctx context.Context, directory, projectName, service, action string) error {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	if err := validateServiceName(service); err != nil {
		return err
	}
	args = append(args, action)
	if service != "" {
		args = append(args, service)
	}
	_, _, err = p.runCompose(ctx, args...)
	return err
}

func (p *CLIProvider) ComposeLogs(ctx context.Context, directory, projectName, service string, tail int, follow bool) (io.ReadCloser, error) {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return nil, err
	}
	if err := validateServiceName(service); err != nil {
		return nil, err
	}
	if tail < 1 {
		tail = 200
	}
	if tail > 5000 {
		tail = 5000
	}
	args = append(args, "logs", "--tail", strconv.Itoa(tail))
	if follow {
		args = append(args, "--follow")
	}
	if service != "" {
		args = append(args, service)
	}
	return p.streamCompose(ctx, args...)
}

func (p *CLIProvider) ComposeHealthy(ctx context.Context, directory, projectName string) error {
	processes, err := p.ComposePS(ctx, directory, projectName)
	if err != nil {
		return err
	}
	if len(processes) == 0 {
		return fmt.Errorf("docker compose has no running services")
	}
	for _, process := range processes {
		state := strings.ToLower(strings.TrimSpace(process.State))
		health := strings.ToLower(strings.TrimSpace(process.Health))
		if state != "running" {
			return fmt.Errorf("docker compose service %s is %s", process.Service, process.State)
		}
		if health == "unhealthy" {
			return fmt.Errorf("docker compose service %s is unhealthy", process.Service)
		}
	}
	return nil
}

func (p *CLIProvider) ComposePS(ctx context.Context, directory, projectName string) ([]ComposeProcess, error) {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return nil, err
	}
	args = append(args, "ps", "--format", "json")
	out, _, err := p.runCompose(ctx, args...)
	if err != nil {
		if composePSFormatUnsupported(err) {
			return p.composePSByInspect(ctx, directory, projectName)
		}
		return nil, err
	}
	var raw []struct {
		Name    string `json:"Name"`
		Service string `json:"Service"`
		State   string `json:"State"`
		Health  string `json:"Health"`
		Image   string `json:"Image"`
	}
	data := strings.TrimSpace(string(out))
	if data == "" {
		return []ComposeProcess{}, nil
	}
	if strings.HasPrefix(data, "[") {
		if err := json.Unmarshal(out, &raw); err != nil {
			return nil, fmt.Errorf("decode docker compose ps: %w", err)
		}
	} else if err := decodeJSONLines(out, &raw); err != nil {
		return nil, fmt.Errorf("decode docker compose ps: %w", err)
	}
	items := make([]ComposeProcess, 0, len(raw))
	for _, item := range raw {
		items = append(items, ComposeProcess{Name: item.Name, Service: item.Service, State: item.State, Health: item.Health, Image: item.Image})
	}
	return items, nil
}

func composeInvocationUnsupported(err error) bool {
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"not a docker command",
		"is not a docker command",
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"flag provided but not defined",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func composePSFormatUnsupported(err error) bool {
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{"unknown flag", "unknown option", "no such option", "flag provided but not defined"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func (p *CLIProvider) composePSByInspect(ctx context.Context, directory, projectName string) ([]ComposeProcess, error) {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return nil, err
	}
	args = append(args, "ps", "-q")
	out, _, err := p.runCompose(ctx, args...)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(out))
	items := make([]ComposeProcess, 0, len(ids))
	for _, id := range ids {
		if err := validateContainerRef(id); err != nil {
			return nil, err
		}
		inspect, _, err := p.runner.Run(ctx, "container", "inspect", id)
		if err != nil {
			return nil, err
		}
		var raw []struct {
			Name   string `json:"Name"`
			Config struct {
				Image  string            `json:"Image"`
				Labels map[string]string `json:"Labels"`
			} `json:"Config"`
			State struct {
				Status string `json:"Status"`
				Health *struct {
					Status string `json:"Status"`
				} `json:"Health"`
			} `json:"State"`
		}
		if err := json.Unmarshal(inspect, &raw); err != nil || len(raw) != 1 {
			if err == nil {
				err = fmt.Errorf("expected one container")
			}
			return nil, fmt.Errorf("decode docker compose container inspect: %w", err)
		}
		item := raw[0]
		health := ""
		if item.State.Health != nil {
			health = item.State.Health.Status
		}
		service := item.Config.Labels["com.docker.compose.service"]
		items = append(items, ComposeProcess{
			Name:    strings.TrimPrefix(item.Name, "/"),
			Service: service,
			State:   item.State.Status,
			Health:  health,
			Image:   item.Config.Image,
		})
	}
	return items, nil
}

func composeArgs(directory, projectName string) ([]string, error) {
	if err := validateProjectName(projectName); err != nil {
		return nil, err
	}
	configFile, err := findComposeFile(directory)
	if err != nil {
		return nil, err
	}
	if err := validateValue(configFile, "compose file"); err != nil {
		return nil, err
	}
	// Keep Compose global options maximally compatible: -p/-f work with both
	// Docker Compose v2 and the legacy docker-compose binary. Do not pass
	// --project-directory; the absolute -f path already gives Compose the
	// directory used for relative paths.
	return withPortOverride(directory, projectName, []string{"compose", "-p", projectName, "-f", configFile})
}

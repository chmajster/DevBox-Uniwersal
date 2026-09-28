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
	_, _, err = p.runner.Run(ctx, args...)
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
	_, _, err = p.runner.Run(ctx, args...)
	return err
}

func (p *CLIProvider) ComposeDown(ctx context.Context, directory, projectName string) error {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	args = append(args, "down")
	_, _, err = p.runner.Run(ctx, args...)
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
	_, _, err = p.runner.Run(ctx, args...)
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
	return p.runner.Stream(ctx, args...)
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
	out, _, err := p.runner.Run(ctx, args...)
	if err != nil {
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

func composeArgs(directory, projectName string) ([]string, error) {
	if err := validateProjectName(projectName); err != nil {
		return nil, err
	}
	configFile, err := findComposeFile(directory)
	if err != nil {
		return nil, err
	}
	dirAbs, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid compose directory", ErrInvalidInput)
	}
	if err := validateValue(dirAbs, "compose directory"); err != nil {
		return nil, err
	}
	return []string{"compose", "--project-name", projectName, "--file", configFile}, nil
}

package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type ComposeApplicationService struct {
	Name    string            `json:"name"`
	Image   string            `json:"image,omitempty"`
	Role    string            `json:"role"`
	Primary bool              `json:"primary"`
	Ports   []int             `json:"ports,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
}

func (p *CLIProvider) ComposeApplicationServices(ctx context.Context, directory, projectName string) ([]ComposeApplicationService, error) {
	data, err := p.composeConfigJSON(ctx, directory, projectName)
	if err != nil {
		return nil, err
	}
	if err := validateApplicationCompose(data, directory); err != nil {
		return nil, err
	}
	var root struct {
		Services map[string]struct {
			Image   string            `json:"image"`
			Labels  map[string]string `json:"labels"`
			Command any               `json:"command"`
			Ports   []struct {
				Target int `json:"target"`
			} `json:"ports"`
			Expose []any `json:"expose"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode compose services: %w", err)
	}
	names := make([]string, 0, len(root.Services))
	for name := range root.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ComposeApplicationService, 0, len(names))
	for _, name := range names {
		raw := root.Services[name]
		targets := map[int]int{}
		for _, port := range raw.Ports {
			if port.Target > 0 && port.Target <= 65535 {
				targets[port.Target] = 0
			}
		}
		for _, item := range raw.Expose {
			value := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(fmt.Sprint(item)), "/tcp"), "/udp")
			if port, err := strconv.Atoi(value); err == nil && port > 0 && port <= 65535 {
				targets[port] = 0
			}
		}
		ports := make([]int, 0, len(targets))
		for port := range targets {
			ports = append(ports, port)
		}
		sort.Ints(ports)
		role := composeApplicationRole(name, raw.Image, raw.Labels, fmt.Sprint(raw.Command), targets)
		primary := parseComposeBool(raw.Labels["io.devbox.primary"])
		out = append(out, ComposeApplicationService{Name: name, Image: raw.Image, Role: role, Primary: primary, Ports: ports, Labels: raw.Labels})
	}
	return out, nil
}

func composeApplicationRole(name, image string, labels map[string]string, command string, ports map[int]int) string {
	if explicit := strings.ToLower(strings.TrimSpace(labels["io.devbox.role"])); explicit != "" {
		switch explicit {
		case "web", "api", "admin", "worker", "scheduler", "database", "cache", "queue", "search", "internal":
			return explicit
		}
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	imageBase := composeImageBaseName(image)
	tokens := func(value string) []string {
		return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == '/' || r == ':' })
	}
	has := func(options ...string) bool {
		for _, token := range append(tokens(lower), tokens(imageBase)...) {
			for _, option := range options {
				if token == option {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has("mysql", "mariadb", "postgres", "postgresql", "database", "db"):
		return "database"
	case has("redis", "memcached", "cache"):
		return "cache"
	case has("rabbitmq", "amqp", "queue"):
		return "queue"
	case has("elasticsearch", "opensearch", "search"):
		return "search"
	case has("worker", "celery"):
		return "worker"
	case has("scheduler", "cron"):
		return "scheduler"
	case has("api", "backend"):
		return "api"
	case has("admin"):
		return "admin"
	case has("web", "frontend", "www", "nginx", "apache", "app", "application"):
		return "web"
	}
	lowerCommand := strings.ToLower(command)
	if strings.Contains(lowerCommand, "worker") || strings.Contains(lowerCommand, "celery") {
		return "worker"
	}
	if strings.Contains(lowerCommand, "scheduler") || strings.Contains(lowerCommand, "cron") {
		return "scheduler"
	}
	if composeServiceIsInfrastructure(name, image, ports) {
		return "internal"
	}
	for port := range ports {
		if composeApplicationPorts[port] {
			return "web"
		}
	}
	return "internal"
}

func parseComposeBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func (p *CLIProvider) ComposeUpApplication(ctx context.Context, directory, projectName string, labelsByService map[string]map[string]string) error {
	return p.ComposeUpApplicationEnvironment(ctx, directory, projectName, labelsByService, nil)
}

func (p *CLIProvider) ComposeUpApplicationEnvironment(ctx context.Context, directory, projectName string, labelsByService map[string]map[string]string, environment map[string]map[string]string) error {
	return p.composeUpApplication(ctx, directory, projectName, labelsByService, environment, nil, false)
}
func (p *CLIProvider) ComposeRecreateApplication(ctx context.Context, directory, projectName string, labelsByService map[string]map[string]string, environment map[string]map[string]string) error {
	return p.composeUpApplication(ctx, directory, projectName, labelsByService, environment, nil, true)
}
func (p *CLIProvider) composeUpApplication(ctx context.Context, directory, projectName string, labelsByService map[string]map[string]string, environment map[string]map[string]string, commands map[string][]string, recreate bool, settings ...map[string]any) error {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	cleanup := func() {}
	if len(labelsByService) > 0 {
		services := map[string]any{}
		for name, labels := range labelsByService {
			if err := validateServiceName(name); err != nil {
				return err
			}
			safe := map[string]string{}
			for key, value := range labels {
				if strings.TrimSpace(key) == "" || strings.Contains(key, "=") {
					return fmt.Errorf("%w: invalid Compose label key", ErrInvalidInput)
				}
				if err := validateValue(key, "label name"); err != nil {
					return err
				}
				if err := validateValue(value, "label value"); err != nil {
					return err
				}
				safe[key] = value
			}
			entry := map[string]any{"labels": safe}
			if command := commands[name]; len(command) > 0 {
				entry["command"] = command
			}
			if id := safe["devbox.project_id"]; id != "" {
				entry["container_name"] = "devbox-" + id + "-" + name
			}
			if values := environment[name]; len(values) > 0 {
				env := map[string]string{}
				for key, value := range values {
					if err := validateValue(key, "environment name"); err != nil {
						return err
					}
					env[key] = strings.ReplaceAll(value, "$", "$$")
				}
				entry["environment"] = env
			}
			services[name] = entry
		}
		resourceLabels := map[string]string{}
		type resource struct {
			External bool `json:"external"`
		}
		var original struct {
			Networks map[string]resource `json:"networks"`
			Volumes  map[string]resource `json:"volumes"`
			Services map[string]struct {
				Image      string `json:"image"`
				WorkingDir string `json:"working_dir"`
				Build      *struct {
					Context    string `json:"context"`
					Dockerfile string `json:"dockerfile"`
				} `json:"build"`
				Networks map[string]any   `json:"networks"`
				Volumes  []map[string]any `json:"volumes"`
			} `json:"services"`
		}
		data, err := p.composeConfigJSON(ctx, directory, projectName)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &original); err != nil {
			return err
		}
		if err := validateApplicationCompose(data, directory); err != nil {
			return err
		}
		for name, service := range original.Services {
			mounts := []map[string]any{}
			if entry, ok := services[name].(map[string]any); ok && len(settings) > 0 && !composeServiceIsInfrastructure(name, service.Image, nil) {
				config := settings[0]
				if restart, _ := config["restart_policy"].(string); restart != "" {
					entry["restart"] = restart
				}
				primary, _ := config["compose_service"].(string)
				if name == primary || service.Build != nil || service.WorkingDir != "" {
					if workdir, _ := config["working_directory"].(string); workdir != "" {
						if name == primary {
							entry["working_dir"] = workdir
						}
					}
					target := ""
					if name == primary {
						target, _ = config["mount_target"].(string)
					}
					if target == "" {
						target = service.WorkingDir
					}
					if target == "" && service.Build != nil {
						file := service.Build.Dockerfile
						if file == "" {
							file = "Dockerfile"
						}
						data, _ := os.ReadFile(filepath.Join(service.Build.Context, file))
						for _, line := range strings.Split(string(data), "\n") {
							fields := strings.Fields(line)
							if len(fields) == 2 && strings.EqualFold(fields[0], "WORKDIR") && strings.HasPrefix(fields[1], "/") {
								target = fields[1]
							}
						}
					}
					if target != "" && target != "/" {
						mounts = append(mounts, map[string]any{"type": "bind", "source": directory, "target": target, "read_only": false})
					}
				}
			}
			for _, mount := range service.Volumes {
				source, _ := mount["source"].(string)
				kind, _ := mount["type"].(string)
				relative, err := filepath.Rel(directory, source)
				if kind == "bind" && err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					copyMount := map[string]any{}
					for key, value := range mount {
						copyMount[key] = value
					}
					copyMount["read_only"] = false
					mounts = append(mounts, copyMount)
				}
			}
			if len(mounts) > 0 {
				if entry, ok := services[name].(map[string]any); ok {
					entry["volumes"] = mounts
				}
			}
			if p.sharedAppNetwork != "" && !composeServiceIsInfrastructure(name, service.Image, nil) {
				if err := p.EnsureNetwork(ctx, p.sharedAppNetwork); err != nil {
					return err
				}
				if entry, ok := services[name].(map[string]any); ok {
					nets := map[string]any{}
					for net, settings := range service.Networks {
						nets[net] = settings
					}
					nets["devbox_shared"] = map[string]any{}
					entry["networks"] = nets
				}
			}
		}

		for _, labels := range labelsByService {
			for _, key := range []string{"devbox.managed", "devbox.project_id", "devbox.project_name"} {
				resourceLabels[key] = labels[key]
			}
			break
		}
		networks := map[string]any{}
		if p.sharedAppNetwork != "" {
			networks["devbox_shared"] = map[string]any{"name": p.sharedAppNetwork, "external": true}
		}
		volumes := map[string]any{}
		for name, item := range original.Networks {
			if !item.External {
				networks[name] = map[string]any{"name": projectName + "_" + name, "labels": resourceLabels}
			}
		}
		for name, item := range original.Volumes {
			if !item.External {
				volumes[name] = map[string]any{"name": projectName + "_" + name, "labels": resourceLabels}
			}
		}
		payload, _ := json.Marshal(map[string]any{"services": services, "networks": networks, "volumes": volumes})
		file, err := os.CreateTemp("", "devbox-compose-labels-*.json")
		if err != nil {
			return err
		}
		path := file.Name()
		cleanup = func() { _ = os.Remove(path) }
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			cleanup()
			return err
		}
		if _, err := file.Write(payload); err != nil {
			_ = file.Close()
			cleanup()
			return err
		}
		if err := file.Close(); err != nil {
			cleanup()
			return err
		}
		args = append(args, "-f", path)
	}
	defer cleanup()
	args = append(args, "up", "--detach", "--remove-orphans")
	if recreate {
		args = append(args, "--force-recreate")
	}
	_, _, err = p.runCompose(ctx, args...)
	return err
}

func (p *CLIProvider) ComposeUpHostingApplication(ctx context.Context, directory, projectName string, labels, environment map[string]map[string]string, commands map[string][]string, recreate bool, config map[string]any) error {
	return p.composeUpApplication(ctx, directory, projectName, labels, environment, commands, recreate, config)
}

func (p *CLIProvider) ComposeStart(ctx context.Context, directory, projectName string) error {
	return p.composeCommand(ctx, directory, projectName, "", "start")
}
func (p *CLIProvider) ComposeStop(ctx context.Context, directory, projectName string) error {
	return p.composeCommand(ctx, directory, projectName, "", "stop")
}

func (p *CLIProvider) ComposePullApplication(ctx context.Context, directory, projectName string) error {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	args = append(args, "pull", "--ignore-buildable")
	_, _, err = p.runCompose(ctx, args...)
	return err
}

func (p *CLIProvider) ComposeUpConfiguredApplication(ctx context.Context, directory, projectName string, labels, environment map[string]map[string]string, commands map[string][]string, recreate bool) error {
	return p.composeUpApplication(ctx, directory, projectName, labels, environment, commands, recreate)
}

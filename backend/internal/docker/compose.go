package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type composeRunner struct {
	runner commandRunner
	legacy bool
}

type environmentCommandRunner interface {
	RunEnv(ctx context.Context, environment map[string]string, args ...string) ([]byte, []byte, error)
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

func (p *CLIProvider) runComposeEnv(ctx context.Context, environment map[string]string, args ...string) ([]byte, []byte, error) {
	resolved, err := p.resolveComposeRunner(ctx)
	if err != nil {
		return nil, nil, err
	}
	envRunner, ok := resolved.runner.(environmentCommandRunner)
	if !ok {
		return nil, nil, fmt.Errorf("%w: Docker runner does not support environment injection", ErrUnavailable)
	}
	if resolved.legacy {
		if len(args) == 0 || args[0] != "compose" {
			return nil, nil, fmt.Errorf("%w: invalid compose invocation", ErrInvalidInput)
		}
		return envRunner.RunEnv(ctx, environment, args[1:]...)
	}
	return envRunner.RunEnv(ctx, environment, args...)
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

func (p *CLIProvider) ComposeUp(ctx context.Context, directory, projectName, service string, binding *providers.ComposePortBinding) error {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	cleanup := func() {}
	if binding != nil {
		effectiveConfig, effectiveCleanup, configErr := p.composeEffectiveConfig(ctx, directory, projectName, *binding)
		if configErr != nil {
			return configErr
		}
		cleanup = effectiveCleanup
		args = []string{"compose", "-p", projectName, "-f", effectiveConfig}
	}
	defer cleanup()
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

func (p *CLIProvider) ComposePortBinding(ctx context.Context, directory, projectName string) (providers.ComposePortBinding, error) {
	data, err := p.composeConfigJSON(ctx, directory, projectName)
	if err != nil {
		return providers.ComposePortBinding{}, err
	}
	return selectComposePortBinding(data)
}

func (p *CLIProvider) composeConfigJSON(ctx context.Context, directory, projectName string) ([]byte, error) {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return nil, err
	}
	args = append(args, "config", "--format", "json")
	out, _, err := p.runCompose(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("read normalized compose config as JSON: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, fmt.Errorf("docker compose returned an empty normalized config")
	}
	return out, nil
}

func selectComposePortBinding(data []byte) (providers.ComposePortBinding, error) {
	discovery, err := discoverComposeApplicationPorts(data, "", 0)
	if err != nil {
		return providers.ComposePortBinding{}, err
	}
	if discovery.Selected == nil {
		if len(discovery.Candidates) == 0 {
			return providers.ComposePortBinding{}, fmt.Errorf("no HTTP application port found in compose config")
		}
		items := make([]string, 0, len(discovery.Candidates))
		for _, candidate := range discovery.Candidates {
			items = append(items, fmt.Sprintf("%s:%d", candidate.Service, candidate.ContainerPort))
		}
		return providers.ComposePortBinding{}, fmt.Errorf("multiple application ports require configuration: %s", strings.Join(items, ", "))
	}
	selected := *discovery.Selected
	if selected.HostPort < 1 || selected.HostPort > 65535 {
		return providers.ComposePortBinding{}, fmt.Errorf("application service %s:%d is not published on the host", selected.Service, selected.ContainerPort)
	}
	return providers.ComposePortBinding{
		Service: selected.Service, RequestedHostPort: selected.HostPort, HostPort: selected.HostPort,
		ContainerPort: selected.ContainerPort, Protocol: "tcp",
	}, nil
}

func composeJSONPort(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return number
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		value, _ := strconv.Atoi(strings.TrimSpace(text))
		return value
	}
	return 0
}

func (p *CLIProvider) composeEffectiveConfig(ctx context.Context, directory, projectName string, binding providers.ComposePortBinding) (string, func(), error) {
	if binding.HostPort < 1 || binding.HostPort > 65535 || binding.ContainerPort < 1 || binding.ContainerPort > 65535 {
		return "", func() {}, fmt.Errorf("%w: invalid compose port binding", ErrInvalidInput)
	}
	data, err := p.composeConfigJSON(ctx, directory, projectName)
	if err != nil {
		return "", func() {}, err
	}
	rewritten, err := rewriteComposePublishedPort(data, binding)
	if err != nil {
		return "", func() {}, err
	}
	file, err := os.CreateTemp("", "devbox-compose-effective-*.json")
	if err != nil {
		return "", func() {}, fmt.Errorf("create effective compose config: %w", err)
	}
	name := file.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("secure effective compose config: %w", err)
	}
	if _, err := file.Write(rewritten); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("write effective compose config: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close effective compose config: %w", err)
	}
	return name, cleanup, nil
}

func rewriteComposePublishedPort(data []byte, binding providers.ComposePortBinding) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode normalized compose config for port rewrite: %w", err)
	}
	services, ok := root["services"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("normalized compose config has no services")
	}
	service, ok := services[binding.Service].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("compose service %q was not found in normalized config", binding.Service)
	}
	ports, ok := service["ports"].([]any)
	if !ok {
		return nil, fmt.Errorf("compose service %q has no published ports", binding.Service)
	}
	updated := false
	for _, entry := range ports {
		port, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		target := composeAnyPort(port["target"])
		published := composeAnyPort(port["published"])
		protocol, _ := port["protocol"].(string)
		protocol = strings.ToLower(strings.TrimSpace(protocol))
		if protocol == "" {
			protocol = "tcp"
		}
		if target == binding.ContainerPort && published == binding.RequestedHostPort && protocol == binding.Protocol {
			port["published"] = strconv.Itoa(binding.HostPort)
			updated = true
			break
		}
	}
	if !updated {
		return nil, fmt.Errorf(
			"compose port mapping %s:%d->%d/%s was not found in normalized config",
			binding.Service, binding.RequestedHostPort, binding.ContainerPort, binding.Protocol,
		)
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("encode effective compose config: %w", err)
	}
	return out, nil
}

func composeAnyPort(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := strconv.Atoi(typed.String())
		return parsed
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
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
		if health != "" && health != "healthy" {
			return fmt.Errorf("docker compose service %s health is %s", process.Service, health)
		}
	}
	return nil
}

func (p *CLIProvider) ComposePS(ctx context.Context, directory, projectName string) ([]ComposeProcess, error) {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return nil, err
	}
	args = append(args, "ps", "--all", "--format", "json")
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

type composePortCandidate struct {
	service       string
	containerPort int
	hostPort      int
	score         int
}

func (p *CLIProvider) ComposeTargetPort(ctx context.Context, directory, projectName string) (int, error) {
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return 0, err
	}
	args = append(args, "ps", "-q")
	out, _, err := p.runCompose(ctx, args...)
	if err != nil {
		return 0, err
	}

	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return 0, fmt.Errorf("docker compose has no containers to inspect for published ports")
	}

	byHostPort := map[int]composePortCandidate{}
	for _, id := range ids {
		if err := validateContainerRef(id); err != nil {
			return 0, err
		}
		inspect, _, err := p.runner.Run(ctx, "container", "inspect", id)
		if err != nil {
			return 0, err
		}
		var raw []struct {
			Config struct {
				Labels map[string]string `json:"Labels"`
			} `json:"Config"`
			NetworkSettings struct {
				Ports map[string][]struct {
					HostIP   string `json:"HostIp"`
					HostPort string `json:"HostPort"`
				} `json:"Ports"`
			} `json:"NetworkSettings"`
		}
		if err := json.Unmarshal(inspect, &raw); err != nil || len(raw) != 1 {
			if err == nil {
				err = fmt.Errorf("expected one container")
			}
			return 0, fmt.Errorf("decode docker compose port inspect: %w", err)
		}

		service := raw[0].Config.Labels["com.docker.compose.service"]
		for containerKey, bindings := range raw[0].NetworkSettings.Ports {
			containerPortText := strings.SplitN(containerKey, "/", 2)[0]
			containerPort, convErr := strconv.Atoi(containerPortText)
			if convErr != nil || containerPort < 1 || containerPort > 65535 {
				continue
			}
			for _, binding := range bindings {
				hostPort, convErr := strconv.Atoi(strings.TrimSpace(binding.HostPort))
				if convErr != nil || hostPort < 1 || hostPort > 65535 {
					continue
				}
				score := composePortScore(service, containerPort)
				if score < 0 {
					continue
				}
				candidate := composePortCandidate{
					service: service, containerPort: containerPort, hostPort: hostPort, score: score,
				}
				if current, exists := byHostPort[hostPort]; !exists || candidate.score > current.score {
					byHostPort[hostPort] = candidate
				}
			}
		}
	}

	if len(byHostPort) == 0 {
		return 0, fmt.Errorf("no published host port found in running Compose containers; publish the HTTP service port in compose.yaml")
	}
	candidates := make([]composePortCandidate, 0, len(byHostPort))
	for _, candidate := range byHostPort {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].hostPort < candidates[j].hostPort
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) == 1 || candidates[0].score > candidates[1].score {
		return candidates[0].hostPort, nil
	}

	ports := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ports = append(ports, strconv.Itoa(candidate.hostPort))
	}
	return 0, fmt.Errorf("multiple published host ports are equally likely for reverse proxy: %s; configure a project port or healthcheck URL", strings.Join(ports, ", "))
}

func composePortScore(service string, containerPort int) int {
	targets := map[int]int{containerPort: 0}
	if composeServiceIsInfrastructure(service, "", targets) {
		return -10000
	}
	score := 10
	if composeApplicationServiceNames[strings.ToLower(strings.TrimSpace(service))] {
		score += 100
	}
	if composeApplicationPorts[containerPort] {
		if containerPort == 443 || containerPort == 8443 {
			score += 50
		} else {
			score += 60
		}
	}
	return score
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
	args = append(args, "ps", "--all", "-q")
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
	args, err := withPortOverride(directory, projectName, []string{"compose", "-p", projectName, "-f", configFile})
	if err != nil {
		return nil, err
	}
	return withDatabaseOverride(directory, projectName, args)
}

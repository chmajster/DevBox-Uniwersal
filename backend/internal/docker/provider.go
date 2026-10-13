package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type CLIProvider struct {
	sharedAppNetwork    string
	runtimeRoot         string
	runner              commandRunner
	legacyComposeRunner commandRunner
}

func (p *CLIProvider) WithSharedAppNetwork(name string) *CLIProvider {
	p.sharedAppNetwork = name
	return p
}

func NewCLIProvider() *CLIProvider {
	return &CLIProvider{
		runner:              execRunner{binary: "docker"},
		legacyComposeRunner: execRunner{binary: "docker-compose"},
	}
}

func newCLIProviderWithRunner(r commandRunner) *CLIProvider {
	return &CLIProvider{runner: r}
}

func newCLIProviderWithComposeRunners(dockerRunner, legacyComposeRunner commandRunner) *CLIProvider {
	return &CLIProvider{runner: dockerRunner, legacyComposeRunner: legacyComposeRunner}
}

func (p *CLIProvider) WithRuntimeRoot(root string) *CLIProvider { p.runtimeRoot = root; return p }

var _ providers.DockerProvider = (*CLIProvider)(nil)

func (p *CLIProvider) Available(ctx context.Context) error {
	_, _, err := p.runner.Run(ctx, "info", "--format", "{{.ServerVersion}}")
	if err != nil {
		return err
	}
	return nil
}

func (p *CLIProvider) Version(ctx context.Context) (DockerVersion, error) {
	out, _, err := p.runner.Run(ctx, "version", "--format", "{{json .}}")
	if err != nil {
		return DockerVersion{}, err
	}
	var raw struct {
		Client struct {
			Version string `json:"Version"`
		} `json:"Client"`
		Server struct {
			Version string `json:"Version"`
		} `json:"Server"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return DockerVersion{}, fmt.Errorf("decode docker version: %w", err)
	}
	return DockerVersion{Client: raw.Client.Version, Server: raw.Server.Version}, nil
}

func (p *CLIProvider) Status(ctx context.Context) (Status, error) {
	status := Status{}
	version, versionErr := p.Version(ctx)
	if versionErr == nil {
		status.ClientVersion = version.Client
		status.ServerVersion = version.Server
	} else {
		client, _, clientErr := p.runner.Run(ctx, "--version")
		if clientErr == nil {
			status.ClientVersion = strings.TrimSpace(string(client))
		}
	}
	info, _, err := p.runner.Run(ctx, "info", "--format", "{{json .}}")
	if err != nil {
		status.Error = err.Error()
		return status, err
	}
	var raw struct {
		Name              string `json:"Name"`
		ServerVersion     string `json:"ServerVersion"`
		OperatingSystem   string `json:"OperatingSystem"`
		OSType            string `json:"OSType"`
		Architecture      string `json:"Architecture"`
		DockerRootDir     string `json:"DockerRootDir"`
		NCPU              int    `json:"NCPU"`
		MemTotal          int64  `json:"MemTotal"`
		Containers        int    `json:"Containers"`
		ContainersRunning int    `json:"ContainersRunning"`
		ContainersStopped int    `json:"ContainersStopped"`
		Images            int    `json:"Images"`
	}
	if err := json.Unmarshal(info, &raw); err != nil {
		return status, fmt.Errorf("decode docker info: %w", err)
	}
	status.Available = true
	status.EngineName = raw.Name
	if status.ServerVersion == "" {
		status.ServerVersion = raw.ServerVersion
	}
	status.OperatingSystem = raw.OperatingSystem
	status.OSType = raw.OSType
	status.Architecture = raw.Architecture
	status.DockerRootDir = raw.DockerRootDir
	status.CPUs = raw.NCPU
	status.MemoryBytes = raw.MemTotal
	status.Containers = raw.Containers
	status.ContainersRunning = raw.ContainersRunning
	status.ContainersStopped = raw.ContainersStopped
	status.Images = raw.Images
	return status, nil
}

func (p *CLIProvider) ListContainers(ctx context.Context) ([]Container, error) {
	out, _, err := p.runner.Run(ctx, "container", "ls", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID        string `json:"ID"`
		Names     string `json:"Names"`
		Image     string `json:"Image"`
		State     string `json:"State"`
		Status    string `json:"Status"`
		Ports     string `json:"Ports"`
		CreatedAt string `json:"CreatedAt"`
		Labels    string `json:"Labels"`
	}
	if err := decodeJSONLines(out, &raw); err != nil {
		return nil, fmt.Errorf("decode docker container list: %w", err)
	}
	items := make([]Container, 0, len(raw))
	for _, item := range raw {
		items = append(items, Container{
			ID: item.ID, Name: item.Names, Image: item.Image, State: item.State, Status: item.Status, Ports: item.Ports, CreatedAt: item.CreatedAt,
			ComposeProject: dockerListLabel(item.Labels, "com.docker.compose.project"),
			ProjectID:      dockerListLabel(item.Labels, "io.devbox.project"),
		})
	}
	return items, nil
}

func dockerListLabel(labels, key string) string {
	for _, entry := range strings.Split(labels, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}

func (p *CLIProvider) InspectContainer(ctx context.Context, id string) (ContainerDetail, error) {
	if err := validateContainerRef(id); err != nil {
		return ContainerDetail{}, err
	}
	out, _, err := p.runner.Run(ctx, "container", "inspect", id)
	if err != nil {
		return ContainerDetail{}, err
	}
	var raw []struct {
		ID              string `json:"Id"`
		Name            string `json:"Name"`
		NetworkSettings struct {
			Ports map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		State struct {
			Status     string `json:"Status"`
			Running    bool   `json:"Running"`
			StartedAt  string `json:"StartedAt"`
			FinishedAt string `json:"FinishedAt"`
			Health     struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
	}
	if err := json.Unmarshal(out, &raw); err != nil || len(raw) != 1 {
		if err == nil {
			err = fmt.Errorf("expected one container")
		}
		return ContainerDetail{}, fmt.Errorf("decode docker container inspect: %w", err)
	}
	item := raw[0]
	bindings := []providers.ContainerPortBinding{}
	for target, values := range item.NetworkSettings.Ports {
		port, _ := strconv.Atoi(strings.TrimSuffix(target, "/tcp"))
		if !strings.HasSuffix(target, "/tcp") {
			continue
		}
		for _, value := range values {
			host, _ := strconv.Atoi(value.HostPort)
			bindings = append(bindings, providers.ContainerPortBinding{HostIP: value.HostIP, HostPort: host, ContainerPort: port})
		}
	}
	return ContainerDetail{PortBindings: bindings,
		Container: Container{
			ID: item.ID, Name: strings.TrimPrefix(item.Name, "/"), Image: item.Config.Image, State: item.State.Status, Status: item.State.Status,
			ComposeProject: item.Config.Labels["com.docker.compose.project"],
			ProjectID:      item.Config.Labels["io.devbox.project"],
		},
		Running: item.State.Running, Health: item.State.Health.Status, StartedAt: item.State.StartedAt, FinishedAt: item.State.FinishedAt, Labels: item.Config.Labels,
	}, nil
}

func (p *CLIProvider) Create(ctx context.Context, spec providers.ContainerSpec) (providers.ContainerInfo, error) {
	if err := validateContainerRef(spec.Name); err != nil {
		return providers.ContainerInfo{}, err
	}
	if err := validateImageRef(spec.Image); err != nil {
		return providers.ContainerInfo{}, err
	}
	if err := validateRestartPolicy(spec.RestartPolicy); err != nil {
		return providers.ContainerInfo{}, err
	}
	args := []string{"container", "create", "--name", spec.Name}
	if spec.RestartPolicy != "" && spec.RestartPolicy != "no" {
		args = append(args, "--restart", spec.RestartPolicy)
	}
	for i, network := range spec.Networks {
		if err := validateNetworkRef(network); err != nil {
			return providers.ContainerInfo{}, err
		}
		if i == 0 {
			args = append(args, "--network", network)
		}
	}
	envKeys := sortedKeys(spec.Environment)
	for _, key := range envKeys {
		if err := validateEnvironmentName(key); err != nil {
			return providers.ContainerInfo{}, err
		}
		if err := validateValue(spec.Environment[key], "environment value"); err != nil {
			return providers.ContainerInfo{}, err
		}
		args = append(args, "--env", key+"="+spec.Environment[key])
	}
	envFile, cleanupEnv, err := secureEnvFile(spec.SensitiveEnvironment)
	if err != nil {
		return providers.ContainerInfo{}, err
	}
	defer cleanupEnv()
	if envFile != "" {
		args = append(args, "--env-file", envFile)
	}
	labelKeys := sortedKeys(spec.Labels)
	for _, key := range labelKeys {
		if err := validateValue(key, "label name"); err != nil {
			return providers.ContainerInfo{}, err
		}
		if err := validateValue(spec.Labels[key], "label value"); err != nil {
			return providers.ContainerInfo{}, err
		}
		if strings.TrimSpace(key) == "" || strings.Contains(key, "=") {
			return providers.ContainerInfo{}, fmt.Errorf("%w: invalid label name", ErrInvalidInput)
		}
		args = append(args, "--label", key+"="+spec.Labels[key])
	}
	hostPorts := make([]int, 0, len(spec.Ports))
	for host := range spec.Ports {
		hostPorts = append(hostPorts, host)
	}
	sort.Ints(hostPorts)
	for _, host := range hostPorts {
		containerPort := spec.Ports[host]
		if host < 1 || host > 65535 || containerPort < 1 || containerPort > 65535 {
			return providers.ContainerInfo{}, fmt.Errorf("%w: port out of range", ErrInvalidInput)
		}
		args = append(args, "--publish", strconv.Itoa(host)+":"+strconv.Itoa(containerPort))
	}
	for _, binding := range spec.PortBindings {
		value, err := containerPortBindingArg(binding)
		if err != nil {
			return providers.ContainerInfo{}, err
		}
		args = append(args, "--publish", value)
	}
	volumeKeys := sortedKeys(spec.Volumes)
	for _, source := range volumeKeys {
		target := spec.Volumes[source]
		if err := validateValue(source, "volume source"); err != nil {
			return providers.ContainerInfo{}, err
		}
		if err := validateValue(target, "volume target"); err != nil {
			return providers.ContainerInfo{}, err
		}
		if source == "" || target == "" {
			return providers.ContainerInfo{}, fmt.Errorf("%w: empty volume source or target", ErrInvalidInput)
		}
		args = append(args, "--volume", source+":"+target)
	}
	args = append(args, spec.Image)
	for _, arg := range spec.Command {
		if err := validateValue(arg, "container command argument"); err != nil {
			return providers.ContainerInfo{}, err
		}
		args = append(args, arg)
	}
	out, _, err := p.runner.Run(ctx, args...)
	if err != nil {
		return providers.ContainerInfo{}, err
	}
	id := strings.TrimSpace(string(out))
	for _, network := range spec.Networks[1:] {
		if err := p.ConnectNetwork(ctx, spec.Name, network); err != nil {
			_, _, _ = p.runner.Run(context.Background(), "container", "rm", "-f", spec.Name)
			return providers.ContainerInfo{}, err
		}
	}
	return providers.ContainerInfo{ID: id, Name: spec.Name, State: "created"}, nil
}

func (p *CLIProvider) Start(ctx context.Context, id string) error {
	return p.containerAction(ctx, "start", id)
}

func (p *CLIProvider) Stop(ctx context.Context, id string) error {
	return p.containerAction(ctx, "stop", id)
}

func (p *CLIProvider) Restart(ctx context.Context, id string) error {
	return p.containerAction(ctx, "restart", id)
}

func (p *CLIProvider) Remove(ctx context.Context, id string) error {
	return p.containerAction(ctx, "rm", id)
}

func (p *CLIProvider) containerAction(ctx context.Context, action, id string) error {
	if err := validateContainerRef(id); err != nil {
		return err
	}
	_, _, err := p.runner.Run(ctx, "container", action, id)
	return err
}

func (p *CLIProvider) Inspect(ctx context.Context, id string) (providers.ContainerInfo, error) {
	item, err := p.InspectContainer(ctx, id)
	if err != nil {
		return providers.ContainerInfo{}, err
	}
	return providers.ContainerInfo{ID: item.ID, Name: item.Name, Image: item.Image, State: item.State, Health: item.Health, PortBindings: item.PortBindings}, nil
}

func (p *CLIProvider) Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error) {
	if err := validateContainerRef(id); err != nil {
		return nil, err
	}
	if tail < 1 {
		tail = 200
	}
	if tail > 5000 {
		tail = 5000
	}
	args := []string{"container", "logs", "--tail", strconv.Itoa(tail)}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, id)
	return p.runner.Stream(ctx, args...)
}

func (p *CLIProvider) Exec(ctx context.Context, id string, command ExecCommand) (string, error) {
	if err := validateContainerRef(id); err != nil {
		return "", err
	}
	program, args, ok := execDefinition(command)
	if !ok {
		return "", fmt.Errorf("%w: unsupported exec command", ErrInvalidInput)
	}
	cliArgs := []string{"container", "exec", id, program}
	cliArgs = append(cliArgs, args...)
	out, _, err := p.runner.Run(ctx, cliArgs...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (p *CLIProvider) PHPModules(ctx context.Context, id string) ([]string, error) {
	if err := validateContainerRef(id); err != nil {
		return nil, err
	}
	out, _, err := p.runner.Run(ctx, "container", "exec", id, "php", "-m")
	if err != nil {
		return nil, err
	}
	modules := make([]string, 0)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		name := strings.ToLower(line)
		if name == "zend opcache" {
			name = "opcache"
		}
		modules = append(modules, name)
	}
	sort.Strings(modules)
	return modules, nil
}

func execDefinition(command ExecCommand) (string, []string, bool) {
	switch command {
	case ExecEnv:
		return "env", nil, true
	case ExecIdentity:
		return "id", nil, true
	case ExecProcesses:
		return "ps", []string{"-ef"}, true
	case ExecSystem:
		return "uname", []string{"-a"}, true
	case ExecWorkingDirectory:
		return "pwd", nil, true
	default:
		return "", nil, false
	}
}

func (p *CLIProvider) PullImage(ctx context.Context, image string) error {
	if err := validateImageRef(image); err != nil {
		return err
	}
	_, _, err := p.runner.Run(ctx, "image", "pull", image)
	return err
}

func (p *CLIProvider) ListImages(ctx context.Context) ([]Image, error) {
	out, _, err := p.runner.Run(ctx, "image", "ls", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID           string `json:"ID"`
		Repository   string `json:"Repository"`
		Tag          string `json:"Tag"`
		Digest       string `json:"Digest"`
		Size         string `json:"Size"`
		CreatedSince string `json:"CreatedSince"`
	}
	if err := decodeJSONLines(out, &raw); err != nil {
		return nil, fmt.Errorf("decode docker image list: %w", err)
	}
	items := make([]Image, 0, len(raw))
	for _, item := range raw {
		items = append(items, Image{ID: item.ID, Repository: item.Repository, Tag: item.Tag, Digest: item.Digest, Size: item.Size, CreatedSince: item.CreatedSince})
	}
	return items, nil
}

func (p *CLIProvider) InspectImage(ctx context.Context, image string) (map[string]any, error) {
	if err := validateImageRef(image); err != nil {
		return nil, err
	}
	return p.inspectObject(ctx, []string{"image", "inspect", image})
}

func (p *CLIProvider) RemoveImage(ctx context.Context, image string) error {
	if err := validateImageRef(image); err != nil {
		return err
	}
	_, _, err := p.runner.Run(ctx, "image", "rm", image)
	return err
}

func (p *CLIProvider) ListVolumes(ctx context.Context) ([]Volume, error) {
	out, _, err := p.runner.Run(ctx, "volume", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name       string `json:"Name"`
		Driver     string `json:"Driver"`
		Scope      string `json:"Scope"`
		Mountpoint string `json:"Mountpoint"`
	}
	if err := decodeJSONLines(out, &raw); err != nil {
		return nil, fmt.Errorf("decode docker volume list: %w", err)
	}
	items := make([]Volume, 0, len(raw))
	for _, item := range raw {
		items = append(items, Volume{Name: item.Name, Driver: item.Driver, Scope: item.Scope, Mountpoint: item.Mountpoint})
	}
	return items, nil
}

func (p *CLIProvider) InspectVolume(ctx context.Context, name string) (map[string]any, error) {
	if err := validateVolumeRef(name); err != nil {
		return nil, err
	}
	return p.inspectObject(ctx, []string{"volume", "inspect", name})
}

func (p *CLIProvider) RemoveVolume(ctx context.Context, name string) error {
	if err := validateVolumeRef(name); err != nil {
		return err
	}
	_, _, err := p.runner.Run(ctx, "volume", "rm", name)
	return err
}

func (p *CLIProvider) ListNetworks(ctx context.Context) ([]Network, error) {
	out, _, err := p.runner.Run(ctx, "network", "ls", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID       string `json:"ID"`
		Name     string `json:"Name"`
		Driver   string `json:"Driver"`
		Scope    string `json:"Scope"`
		Internal string `json:"Internal"`
		IPv6     string `json:"IPv6"`
	}
	if err := decodeJSONLines(out, &raw); err != nil {
		return nil, fmt.Errorf("decode docker network list: %w", err)
	}
	items := make([]Network, 0, len(raw))
	for _, item := range raw {
		items = append(items, Network{ID: item.ID, Name: item.Name, Driver: item.Driver, Scope: item.Scope, Internal: item.Internal, IPv6: item.IPv6})
	}
	return items, nil
}

func (p *CLIProvider) InspectNetwork(ctx context.Context, id string) (map[string]any, error) {
	if err := validateNetworkRef(id); err != nil {
		return nil, err
	}
	return p.inspectObject(ctx, []string{"network", "inspect", id})
}

func (p *CLIProvider) inspectObject(ctx context.Context, args []string) (map[string]any, error) {
	out, _, err := p.runner.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var objects []map[string]any
	if err := json.Unmarshal(out, &objects); err != nil || len(objects) != 1 {
		if err == nil {
			err = fmt.Errorf("expected one object")
		}
		return nil, fmt.Errorf("decode docker inspect: %w", err)
	}
	return objects[0], nil
}

func decodeJSONLines[T any](data []byte, target *[]T) error {
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item T
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return err
		}
		*target = append(*target, item)
	}
	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

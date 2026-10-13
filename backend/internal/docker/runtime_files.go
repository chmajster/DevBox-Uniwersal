package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

// SaveManagedRuntime records a reviewable Docker configuration outside source.
// Secrets are injected at container creation through a temporary 0600 env file.
func (p *CLIProvider) SaveManagedRuntime(spec containerspec.DeploymentSpec) error {
	if p.runtimeRoot == "" {
		return nil
	}
	if err := validateProjectName(spec.ProjectID); err != nil {
		return err
	}
	dir := filepath.Join(p.runtimeRoot, spec.ProjectID, "runtime")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	volumes := []any{}
	for source, target := range spec.BindMounts {
		volumes = append(volumes, map[string]any{"type": "bind", "source": source, "target": target, "read_only": false})
	}
	sort.Slice(volumes, func(i, j int) bool { return fmt.Sprint(volumes[i]) < fmt.Sprint(volumes[j]) })
	for _, target := range spec.AnonymousVolumes {
		volumes = append(volumes, map[string]any{"type": "volume", "target": target})
	}
	networks := map[string]any{}
	for _, name := range spec.Networks {
		networks[name] = map[string]any{"name": name, "external": true}
	}
	service := map[string]any{"image": spec.Image, "container_name": spec.ContainerName, "volumes": volumes, "labels": spec.Labels, "environment": spec.Environment, "networks": spec.Networks, "ports": []string{fmt.Sprintf("%d:%d", spec.HostPort, spec.ContainerPort)}, "restart": restartPolicy(spec.RestartPolicy), "security_opt": []string{"no-new-privileges:true"}, "cap_drop": []string{"ALL"}}
	if spec.WorkingDirectory != "" {
		service["working_dir"] = spec.WorkingDirectory
	}
	if spec.Dockerfile != "" || spec.DockerfilePath != "" {
		file := filepath.Join(dir, "Dockerfile")
		if spec.DockerfilePath != "" {
			file = spec.DockerfilePath
		}
		service["build"] = map[string]string{"context": spec.ContextDir, "dockerfile": file}
	}
	if spec.User != "" {
		service["user"] = spec.User
	}
	if len(spec.Command) > 0 {
		service["command"] = spec.Command
	}
	if len(spec.Capabilities) > 0 {
		service["cap_add"] = spec.Capabilities
	}
	config, err := json.MarshalIndent(map[string]any{"services": map[string]any{"web": service}, "networks": networks}, "", "  ")
	if err != nil {
		return err
	}
	metadata, err := json.MarshalIndent(map[string]any{"runtime": spec.Runtime, "version": spec.Version, "fingerprint": spec.Fingerprint, "container": spec.ContainerName, "source_mounts": spec.BindMounts, "container_port": spec.ContainerPort, "host_port": spec.HostPort, "command": spec.Command}, "", "  ")
	if err != nil {
		return err
	}
	dockerfile := []byte(spec.Dockerfile)
	if spec.DockerfilePath != "" {
		var err error
		dockerfile, err = os.ReadFile(spec.DockerfilePath)
		if err != nil {
			return err
		}
	}
	for name, data := range map[string][]byte{"Dockerfile": dockerfile, "compose.yaml": config, "metadata.json": metadata} {
		if err := atomicRuntimeFile(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	return nil
}

func restartPolicy(value string) string {
	if value == "" {
		return "unless-stopped"
	}
	return value
}

func atomicRuntimeFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (p *CLIProvider) EnsureApplicationNetwork(ctx context.Context, name string, labels map[string]string) error {
	if err := validateNetworkRef(name); err != nil {
		return err
	}
	out, _, err := p.runner.Run(ctx, "network", "inspect", "--format", "{{json .Labels}}", name)
	if err == nil {
		var actual map[string]string
		if json.Unmarshal(out, &actual) != nil || actual["devbox.project_id"] != labels["devbox.project_id"] {
			return fmt.Errorf("network belongs to another owner")
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	args := []string{"network", "create"}
	for _, key := range []string{"devbox.managed", "devbox.project_id", "devbox.project_name"} {
		if strings.ContainsRune(labels[key], '\x00') {
			return ErrInvalidInput
		}
		args = append(args, "--label", key+"="+labels[key])
	}
	args = append(args, name)
	_, _, err = p.runner.Run(ctx, args...)
	return err
}

func (p *CLIProvider) RemoveApplicationNetwork(ctx context.Context, name string) error {
	if err := validateNetworkRef(name); err != nil {
		return err
	}
	out, _, err := p.runner.Run(ctx, "network", "inspect", "--format", "{{json .Labels}}", name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var labels map[string]string
	if json.Unmarshal(out, &labels) != nil || name != "devbox-"+labels["devbox.project_id"] {
		return fmt.Errorf("refusing to remove another owner's network")
	}
	_, _, err = p.runner.Run(ctx, "network", "rm", name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (p *CLIProvider) RemoveRuntimeFiles(id string) error {
	if p.runtimeRoot == "" {
		return nil
	}
	if err := validateProjectName(id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(p.runtimeRoot, id, "runtime"))
}

package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateApplicationCompose(data []byte, directory string) error {
	var config struct {
		Services map[string]struct {
			Privileged  bool                                    `json:"privileged"`
			NetworkMode string                                  `json:"network_mode"`
			PID         string                                  `json:"pid"`
			IPC         string                                  `json:"ipc"`
			Devices     []any                                   `json:"devices"`
			CapAdd      []string                                `json:"cap_add"`
			Volumes     []struct{ Type, Source, Target string } `json:"volumes"`
			Build       *struct {
				Context            string            `json:"context"`
				Dockerfile         string            `json:"dockerfile"`
				AdditionalContexts map[string]string `json:"additional_contexts"`
				Privileged         bool              `json:"privileged"`
				Entitlements       []string          `json:"entitlements"`
				SSH                []any             `json:"ssh"`
			} `json:"build"`
		} `json:"services"`
		Volumes map[string]struct {
			Driver     string         `json:"driver"`
			DriverOpts map[string]any `json:"driver_opts"`
		} `json:"volumes"`
		Secrets map[string]struct {
			File string `json:"file"`
		} `json:"secrets"`
		Configs map[string]struct {
			File string `json:"file"`
		} `json:"configs"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	base, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	for name, volume := range config.Volumes {
		if len(volume.DriverOpts) > 0 || (volume.Driver != "" && volume.Driver != "local") {
			return fmt.Errorf("%w: Compose volume %s requests an unmanaged host/storage mount", ErrInvalidInput, name)
		}
	}
	sharedPaths := []string{}
	for _, secret := range config.Secrets {
		if secret.File != "" {
			sharedPaths = append(sharedPaths, secret.File)
		}
	}
	for _, file := range config.Configs {
		if file.File != "" {
			sharedPaths = append(sharedPaths, file.File)
		}
	}
	for name, service := range config.Services {
		if service.Privileged || service.NetworkMode == "host" || service.PID == "host" || service.IPC == "host" || len(service.Devices) > 0 {
			return fmt.Errorf("%w: Compose service %s requests host privileges", ErrInvalidInput, name)
		}
		for _, capability := range service.CapAdd {
			if capability != "NET_BIND_SERVICE" && capability != "SETUID" && capability != "SETGID" {
				return fmt.Errorf("%w: unsafe capability on service %s", ErrInvalidInput, name)
			}
		}
		paths := append([]string(nil), sharedPaths...)
		if service.Build != nil {
			if service.Build.Privileged || len(service.Build.Entitlements) > 0 || len(service.Build.SSH) > 0 {
				return fmt.Errorf("%w: Compose build %s requests host privileges or SSH access", ErrInvalidInput, name)
			}
			paths = append(paths, service.Build.Context)
			if service.Build.Dockerfile != "" {
				file := service.Build.Dockerfile
				if !filepath.IsAbs(file) {
					file = filepath.Join(service.Build.Context, file)
				}
				paths = append(paths, file)
			}
			for _, source := range service.Build.AdditionalContexts {
				if strings.HasPrefix(source, "docker-image://") || strings.HasPrefix(source, "service:") {
					continue
				}
				paths = append(paths, source)
			}
		}
		for _, mount := range service.Volumes {
			if strings.Contains(strings.ToLower(mount.Target), "docker.sock") {
				return fmt.Errorf("%w: Docker socket mounts are forbidden", ErrInvalidInput)
			}
			if mount.Type == "bind" {
				paths = append(paths, mount.Source)
			}
		}
		for _, source := range paths {
			if !filepath.IsAbs(source) {
				source = filepath.Join(base, source)
			}
			resolved, err := filepath.EvalSymlinks(source)
			if err != nil {
				return fmt.Errorf("%w: Compose source is unavailable: %v", ErrInvalidInput, err)
			}
			rel, err := filepath.Rel(base, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("%w: Compose service %s mounts/builds outside its authorized application source", ErrInvalidInput, name)
			}
			for _, segment := range strings.Split(filepath.ToSlash(resolved), "/") {
				if segment == ".ssh" || segment == ".aws" || segment == ".codex" {
					return fmt.Errorf("%w: sensitive Compose source", ErrInvalidInput)
				}
			}
			if info, err := os.Stat(resolved); err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
				return ErrInvalidInput
			}
		}
	}
	return nil
}

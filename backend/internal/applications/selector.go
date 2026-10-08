package applications

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Selector struct{ registry *DriverRegistry }

func NewSelector(registry *DriverRegistry) *Selector { return &Selector{registry: registry} }

func (s *Selector) Detect(ctx context.Context, request DetectRequest, explicitDriver string) (DetectionResult, error) {
	mode := strings.TrimSpace(configString(request.Configuration, "deployment_mode"))
	if mode != "" {
		switch mode {
		case "compose":
			if request.WorkDir == "" || !hasComposeFile(request.WorkDir) {
				return DetectionResult{}, fmt.Errorf("%w: Docker Compose mode requires compose.yaml, compose.yml, docker-compose.yml or docker-compose.yaml in the application root", ErrInvalidInput)
			}
			return s.detectWith(ctx, "compose", request, "Docker Compose selected")
		case "auto":
			if request.WorkDir == "" {
				return DetectionResult{Driver: "managed", RequiresConfiguration: true, Warnings: []string{"source is not available locally for runtime detection"}}, ErrConfigurationRequired
			}
			result, err := s.detectWith(ctx, "managed", request, "DevBox Auto Container selected")
			if err != nil {
				return result, err
			}
			return result, nil
		case "dockerfile", "image":
			return s.detectWith(ctx, "managed", request, mode+" selected")
		default:
			return DetectionResult{}, fmt.Errorf("%w: unsupported deployment_mode", ErrInvalidInput)
		}
	}
	if request.SourceType == SourceImage {
		return s.detectWith(ctx, "managed", request, "OCI image source")
	}
	explicitDriver = strings.TrimSpace(explicitDriver)
	if explicitDriver != "" {
		if explicitDriver != "managed" && explicitDriver != "compose" {
			return DetectionResult{}, fmt.Errorf("%w: only managed and compose drivers are supported", ErrInvalidInput)
		}
		return s.detectWith(ctx, explicitDriver, request, "driver explicitly configured")
	}
	if request.WorkDir == "" {
		return DetectionResult{RequiresConfiguration: true, Warnings: []string{"source is not available locally for detection"}}, ErrConfigurationRequired
	}
	if hasComposeFile(request.WorkDir) {
		return s.detectWith(ctx, "compose", request, "Compose file detected")
	}
	if regularFile(filepath.Join(request.WorkDir, "Dockerfile")) {
		request.Configuration = copyConfiguration(request.Configuration)
		request.Configuration["deployment_mode"] = "dockerfile"
		return s.detectWith(ctx, "managed", request, "Dockerfile detected")
	}
	result, err := s.detectWith(ctx, "managed", request, "supported managed runtime detected")
	if err == nil && result.Runtime != "" {
		return result, nil
	}
	if err != nil && !errors.Is(err, ErrConfigurationRequired) {
		return DetectionResult{}, err
	}
	return DetectionResult{Confidence: "none", RequiresConfiguration: true, Reasons: []string{"no Compose file or supported runtime was detected; select a managed runtime explicitly"}}, ErrConfigurationRequired
}

func copyConfiguration(config map[string]any) map[string]any {
	copy := map[string]any{}
	for key, value := range config {
		copy[key] = value
	}
	return copy
}

func hasComposeFile(root string) bool {
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"} {
		if regularFile(filepath.Join(root, name)) {
			return true
		}
	}
	return false
}

func configString(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}

func (s *Selector) detectWith(ctx context.Context, name string, request DetectRequest, reason string) (DetectionResult, error) {
	driver, ok := s.registry.Get(name)
	if !ok {
		return DetectionResult{}, fmt.Errorf("%w: deployment driver %q", ErrProviderUnavailable, name)
	}
	result, err := driver.Detect(ctx, request)
	if err != nil && !errors.Is(err, ErrConfigurationRequired) {
		return DetectionResult{}, err
	}
	if errors.Is(err, ErrConfigurationRequired) {
		result.RequiresConfiguration = true
	}
	result.ComposeFound = hasComposeFile(request.WorkDir)
	result.DockerfileFound = regularFile(filepath.Join(request.WorkDir, "Dockerfile"))
	result.Driver = name
	result.Reasons = append([]string{reason}, result.Reasons...)
	if result.Confidence == "" {
		result.Confidence = "high"
	}
	return result, nil
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

package applications

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Selector struct{ registry *DriverRegistry }

func NewSelector(registry *DriverRegistry) *Selector { return &Selector{registry: registry} }

func (s *Selector) Detect(ctx context.Context, request DetectRequest, explicitDriver string) (DetectionResult, error) {
	explicitDriver = strings.TrimSpace(explicitDriver)
	if explicitDriver != "" {
		return s.detectWith(ctx, explicitDriver, request, "driver explicitly configured")
	}
	if request.SourceType == SourceDockerImage {
		return s.detectWith(ctx, "image", request, "Docker/OCI image source selected")
	}
	if request.WorkDir == "" {
		return DetectionResult{RequiresConfiguration: true, Warnings: []string{"source is not available locally for detection"}}, ErrConfigurationRequired
	}
	if manifest, err := LoadManifest(request.WorkDir); err != nil {
		return DetectionResult{}, err
	} else if manifest != nil && manifest.Driver != "" {
		return s.detectWith(ctx, manifest.Driver, request, "devbox.yaml selected deployment driver")
	}
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"} {
		if regularFile(filepath.Join(request.WorkDir, name)) {
			return s.detectWith(ctx, "compose", request, name+" detected")
		}
	}
	if regularFile(filepath.Join(request.WorkDir, "Dockerfile")) {
		return s.detectWith(ctx, "dockerfile", request, "Dockerfile detected")
	}
	result, err := s.detectWith(ctx, "managed", request, "supported managed runtime detected")
	if err == nil && result.Runtime != "" {
		return result, nil
	}
	if err != nil && err != ErrConfigurationRequired {
		return DetectionResult{}, err
	}
	return DetectionResult{Confidence: "none", RequiresConfiguration: true, Reasons: []string{"no devbox.yaml, Compose file, Dockerfile or supported runtime was detected"}}, ErrConfigurationRequired
}

func (s *Selector) detectWith(ctx context.Context, name string, request DetectRequest, reason string) (DetectionResult, error) {
	driver, ok := s.registry.Get(name)
	if !ok {
		return DetectionResult{}, fmt.Errorf("%w: deployment driver %q", ErrProviderUnavailable, name)
	}
	result, err := driver.Detect(ctx, request)
	if err != nil {
		return DetectionResult{}, err
	}
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

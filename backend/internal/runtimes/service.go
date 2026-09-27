package runtimes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

type DetectionView struct {
	Runtime               string         `json:"runtime"`
	Framework             string         `json:"framework"`
	Confidence            int            `json:"confidence"`
	DetectedFiles         []string       `json:"detected_files"`
	SuggestedBuildCommand string         `json:"suggested_build_command,omitempty"`
	SuggestedStartCommand string         `json:"suggested_start_command,omitempty"`
	RequiredVersion       string         `json:"required_version,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
}

type ProjectRuntimeView struct {
	ProjectID      string           `json:"project_id"`
	Runtime        string           `json:"runtime"`
	Framework      string           `json:"framework"`
	Confidence     int              `json:"confidence"`
	DetectedFiles  []string         `json:"detected_files"`
	Version        string           `json:"version,omitempty"`
	Availability   string           `json:"availability"`
	Dependencies   []DependencyInfo `json:"dependencies,omitempty"`
	BuildCommand   string           `json:"build_command,omitempty"`
	StartCommand   string           `json:"start_command,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	ConfiguredRuntime string        `json:"configured_runtime,omitempty"`
}

type RuntimeValidationView struct {
	ProjectID    string   `json:"project_id"`
	Runtime      string   `json:"runtime"`
	Availability string   `json:"availability"`
	Version      string   `json:"version,omitempty"`
	Valid        bool     `json:"valid"`
	Warnings     []string `json:"warnings,omitempty"`
	Errors       []string `json:"errors,omitempty"`
}

type Service struct {
	registry Registry
	resolver ProjectResolver
	secrets  secrets.SecretStore
}

func NewService(registry Registry, resolver ProjectResolver, secretStore secrets.SecretStore) *Service {
	return &Service{registry: registry, resolver: resolver, secrets: secretStore}
}

func (s *Service) ListRuntimes(ctx context.Context) []RuntimeInfo {
	result := make([]RuntimeInfo, 0, len(s.registry.List()))
	for _, name := range s.registry.List() {
		runtime, ok := s.registry.Get(name)
		if !ok {
			continue
		}
		if inspector, ok := runtime.(Inspector); ok {
			result = append(result, inspector.Inspect(ctx))
			continue
		}
		result = append(result, RuntimeInfo{Runtime: name, Status: AvailabilityInvalid})
	}
	return result
}

func (s *Service) Detect(ctx context.Context, projectID string) (DetectionView, error) {
	resolved, err := s.resolver.Resolve(ctx, projectID)
	if err != nil {
		return DetectionView{}, err
	}
	detection, _, err := s.bestDetection(ctx, resolved.Context)
	if err != nil {
		return DetectionView{}, err
	}
	return detectionView(detection), nil
}

func (s *Service) ProjectRuntime(ctx context.Context, projectID string) (ProjectRuntimeView, error) {
	resolved, err := s.resolver.Resolve(ctx, projectID)
	if err != nil {
		return ProjectRuntimeView{}, err
	}

	detection, runtime, err := s.selectedRuntime(ctx, resolved)
	if err != nil {
		return ProjectRuntimeView{}, err
	}
	info := RuntimeInfo{Runtime: runtime.Name(), Status: AvailabilityInvalid}
	if inspector, ok := runtime.(Inspector); ok {
		info = inspector.Inspect(ctx)
	}
	view := detectionView(detection)
	return ProjectRuntimeView{
		ProjectID:         projectID,
		Runtime:           view.Runtime,
		Framework:         view.Framework,
		Confidence:        view.Confidence,
		DetectedFiles:     view.DetectedFiles,
		Version:           info.Version,
		Availability:      info.Status,
		Dependencies:      info.Dependencies,
		BuildCommand:      view.SuggestedBuildCommand,
		StartCommand:      view.SuggestedStartCommand,
		Environment:       maskedEnvironment(resolved.Config),
		ConfiguredRuntime: resolved.Config.Runtime,
	}, nil
}

func (s *Service) Validate(ctx context.Context, projectID string) (RuntimeValidationView, error) {
	resolved, err := s.resolver.Resolve(ctx, projectID)
	if err != nil {
		return RuntimeValidationView{}, err
	}

	detection, runtime, err := s.selectedRuntime(ctx, resolved)
	if err != nil {
		return RuntimeValidationView{}, err
	}
	project, secretErrors := s.resolveSecrets(ctx, resolved)
	validation, err := runtime.Validate(ctx, project)
	if err != nil {
		return RuntimeValidationView{}, err
	}
	if len(secretErrors) > 0 {
		validation.Valid = false
		validation.Errors = append(validation.Errors, secretErrors...)
	}
	sort.Strings(validation.Warnings)
	sort.Strings(validation.Errors)

	info := RuntimeInfo{Runtime: runtime.Name(), Status: AvailabilityInvalid}
	if inspector, ok := runtime.(Inspector); ok {
		info = inspector.Inspect(ctx)
	}
	return RuntimeValidationView{
		ProjectID:    projectID,
		Runtime:      detection.Runtime,
		Availability: info.Status,
		Version:      info.Version,
		Valid:        validation.Valid,
		Warnings:     validation.Warnings,
		Errors:       validation.Errors,
	}, nil
}

func (s *Service) selectedRuntime(ctx context.Context, resolved ResolvedProject) (Detection, Runtime, error) {
	if resolved.Config.Runtime == "" {
		return s.bestDetection(ctx, resolved.Context)
	}
	runtime, ok := s.registry.Get(resolved.Config.Runtime)
	if !ok {
		return Detection{}, nil, fmt.Errorf("configured runtime %q is not registered", resolved.Config.Runtime)
	}
	detection, err := runtime.Detect(ctx, resolved.Context)
	if err != nil {
		return Detection{}, nil, err
	}
	if !detection.Detected {
		return Detection{
			Detected: true,
			Runtime:  runtime.Name(),
			Metadata: map[string]any{
				"framework":               runtime.Name(),
				"confidence":              0,
				"detected_files":          []string{},
				"suggested_build_command": "",
				"suggested_start_command": "",
			},
		}, runtime, nil
	}
	return detection, runtime, nil
}

func (s *Service) bestDetection(ctx context.Context, project ProjectContext) (Detection, Runtime, error) {
	type candidate struct {
		detection Detection
		runtime   Runtime
	}
	candidates := make([]candidate, 0)
	var detectionErrors []error
	for _, name := range s.registry.List() {
		runtime, ok := s.registry.Get(name)
		if !ok {
			continue
		}
		detection, err := runtime.Detect(ctx, project)
		if err != nil {
			detectionErrors = append(detectionErrors, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if detection.Detected {
			candidates = append(candidates, candidate{detection: detection, runtime: runtime})
		}
	}
	if len(candidates) == 0 {
		if len(detectionErrors) > 0 {
			return Detection{}, nil, errors.Join(detectionErrors...)
		}
		return Detection{}, nil, ErrRuntimeNotDetected
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := detectionConfidence(candidates[i].detection)
		right := detectionConfidence(candidates[j].detection)
		if left == right {
			return candidates[i].detection.Runtime < candidates[j].detection.Runtime
		}
		return left > right
	})
	return candidates[0].detection, candidates[0].runtime, nil
}

func detectionView(detection Detection) DetectionView {
	metadata := cloneAnyMap(detection.Metadata)
	delete(metadata, "framework")
	delete(metadata, "confidence")
	delete(metadata, "detected_files")
	delete(metadata, "suggested_build_command")
	delete(metadata, "suggested_start_command")
	return DetectionView{
		Runtime:               detection.Runtime,
		Framework:             detectionString(detection, "framework"),
		Confidence:            detectionConfidence(detection),
		DetectedFiles:         detectionStrings(detection, "detected_files"),
		SuggestedBuildCommand: detectionString(detection, "suggested_build_command"),
		SuggestedStartCommand: detectionString(detection, "suggested_start_command"),
		RequiredVersion:       detection.Version,
		Metadata:              metadata,
	}
}

func (s *Service) resolveSecrets(ctx context.Context, resolved ResolvedProject) (ProjectContext, []string) {
	project := resolved.Context
	project.Environment = copyStringMap(resolved.Config.Environment)
	var validationErrors []string
	for key, reference := range resolved.Config.SecretEnvironment {
		scope := reference.Scope
		if scope == "" {
			scope = "project:" + project.ProjectID + ":runtime"
		}
		if s.secrets == nil {
			validationErrors = append(validationErrors, fmt.Sprintf("secret environment %s cannot be resolved because SecretStore is not configured", key))
			continue
		}
		plaintext, err := s.secrets.Get(ctx, scope, reference.Name)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("secret environment %s cannot be resolved", key))
			continue
		}
		project.Environment[key] = string(plaintext)
	}
	return project, validationErrors
}

func maskedEnvironment(config RuntimeConfig) map[string]string {
	result := make(map[string]string)
	for key, value := range config.Environment {
		if sensitiveEnvironmentKey(key) {
			result[key] = "********"
		} else {
			result[key] = value
		}
	}
	for key := range config.SecretEnvironment {
		result[key] = "********"
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func sensitiveEnvironmentKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"PASSWORD", "PASSWD", "SECRET", "TOKEN", "PRIVATE_KEY", "API_KEY"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

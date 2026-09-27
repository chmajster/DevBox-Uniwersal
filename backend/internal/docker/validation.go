package docker

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrUnavailable  = errors.New("docker unavailable")
	ErrInvalidInput = errors.New("invalid docker input")
	ErrNotFound     = errors.New("docker resource not found")

	containerRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	imageRefPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,254}$`)
	projectNamePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	serviceNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	envNamePattern      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func validateContainerRef(value string) error {
	if !containerRefPattern.MatchString(value) {
		return fmt.Errorf("%w: invalid container identifier", ErrInvalidInput)
	}
	return nil
}

func validateImageRef(value string) error {
	if !imageRefPattern.MatchString(value) || strings.Contains(value, "..") || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%w: invalid image reference", ErrInvalidInput)
	}
	return nil
}

func validateVolumeRef(value string) error {
	if !containerRefPattern.MatchString(value) {
		return fmt.Errorf("%w: invalid volume name", ErrInvalidInput)
	}
	return nil
}

func validateNetworkRef(value string) error {
	if !containerRefPattern.MatchString(value) {
		return fmt.Errorf("%w: invalid network identifier", ErrInvalidInput)
	}
	return nil
}

func validateProjectName(value string) error {
	if !projectNamePattern.MatchString(value) {
		return fmt.Errorf("%w: invalid compose project name", ErrInvalidInput)
	}
	return nil
}

func validateServiceName(value string) error {
	if value != "" && !serviceNamePattern.MatchString(value) {
		return fmt.Errorf("%w: invalid compose service name", ErrInvalidInput)
	}
	return nil
}

func validateEnvironmentName(value string) error {
	if !envNamePattern.MatchString(value) {
		return fmt.Errorf("%w: invalid environment variable name", ErrInvalidInput)
	}
	return nil
}

func validateValue(value, label string) error {
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%w: %s contains forbidden control characters", ErrInvalidInput, label)
	}
	return nil
}

func safeChild(root, name string) (string, error) {
	if !containerRefPattern.MatchString(name) {
		return "", fmt.Errorf("%w: invalid project directory name", ErrInvalidInput)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolve projects root", ErrInvalidInput)
	}
	candidate := filepath.Join(rootAbs, name)
	rel, err := filepath.Rel(rootAbs, candidate)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: project path escapes projects root", ErrInvalidInput)
	}
	return candidate, nil
}

func composeProjectName(name string) (string, error) {
	value := strings.ToLower(name)
	value = strings.ReplaceAll(value, ".", "-")
	if err := validateProjectName(value); err != nil {
		return "", err
	}
	return value, nil
}

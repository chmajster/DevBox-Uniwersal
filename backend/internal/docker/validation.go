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

	containerRefPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	imageRefPattern           = regexp.MustCompile(`^(?:[A-Za-z0-9]|\[)[A-Za-z0-9._:/@+\[\]-]{0,254}$`)
	imageNameComponentPattern = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*$`)
	imageTagPattern           = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
	imageDigestPattern        = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[+._-][A-Za-z][A-Za-z0-9]*)*:[A-Fa-f0-9]{32,}$`)
	imageRegistryPattern      = regexp.MustCompile(`^(?:localhost|[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?|\[[A-Fa-f0-9:.]+\])(?::[0-9]{1,5})?$`)
	projectNamePattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	serviceNamePattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	envNamePattern            = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func validateContainerRef(value string) error {
	if !containerRefPattern.MatchString(value) {
		return fmt.Errorf("%w: invalid container identifier", ErrInvalidInput)
	}
	return nil
}

func validateImageRef(value string) error {
	if strings.TrimSpace(value) != value || !imageRefPattern.MatchString(value) || strings.Contains(value, "..") || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%w: invalid image reference", ErrInvalidInput)
	}

	nameAndTag := value
	if strings.Count(value, "@") > 1 {
		return fmt.Errorf("%w: invalid image reference", ErrInvalidInput)
	}
	if at := strings.IndexByte(value, '@'); at >= 0 {
		nameAndTag = value[:at]
		digest := value[at+1:]
		if !imageDigestPattern.MatchString(digest) {
			return fmt.Errorf("%w: invalid image digest", ErrInvalidInput)
		}
	}

	name := nameAndTag
	lastSlash := strings.LastIndexByte(nameAndTag, '/')
	if colon := strings.LastIndexByte(nameAndTag, ':'); colon > lastSlash {
		tag := nameAndTag[colon+1:]
		if !imageTagPattern.MatchString(tag) {
			return fmt.Errorf("%w: invalid image tag", ErrInvalidInput)
		}
		name = nameAndTag[:colon]
	}
	if name == "" {
		return fmt.Errorf("%w: invalid image reference", ErrInvalidInput)
	}

	parts := strings.Split(name, "/")
	pathStart := 0
	if len(parts) > 1 && (strings.ContainsAny(parts[0], ".:") || parts[0] == "localhost" || strings.HasPrefix(parts[0], "[")) {
		if !imageRegistryPattern.MatchString(parts[0]) {
			return fmt.Errorf("%w: invalid image registry", ErrInvalidInput)
		}
		pathStart = 1
	}
	if pathStart >= len(parts) {
		return fmt.Errorf("%w: invalid image reference", ErrInvalidInput)
	}
	for _, part := range parts[pathStart:] {
		if !imageNameComponentPattern.MatchString(part) {
			return fmt.Errorf("%w: invalid image repository component", ErrInvalidInput)
		}
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

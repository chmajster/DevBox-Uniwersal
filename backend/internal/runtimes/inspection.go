package runtimes

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	AvailabilityAvailable = "available"
	AvailabilityMissing   = "missing"
	AvailabilityInvalid   = "invalid"
)

type DependencyInfo struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

type RuntimeInfo struct {
	Runtime      string           `json:"runtime"`
	Status       string           `json:"status"`
	Version      string           `json:"version,omitempty"`
	Dependencies []DependencyInfo `json:"dependencies,omitempty"`
}

type Inspector interface {
	Inspect(ctx context.Context) RuntimeInfo
}

func findExecutable(candidates ...string) (string, error) {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		path, err := exec.LookPath(candidate)
		if err == nil {
			return path, nil
		}

		// systemd and service processes often have a reduced PATH that omits
		// sbin directories. Runtime executables such as php-fpm are commonly
		// installed there, so probe standard system locations as a fallback.
		if !filepath.IsAbs(candidate) && !strings.ContainsAny(candidate, `/\\`) {
			for _, dir := range []string{"/usr/local/sbin", "/usr/sbin", "/usr/local/bin", "/usr/bin", "/sbin", "/bin"} {
				path, err = exec.LookPath(filepath.Join(dir, candidate))
				if err == nil {
					return path, nil
				}
			}
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("runtime executable is not configured")
	}
	return "", fmt.Errorf("runtime executable not found: %s", strings.Join(candidates, ", "))
}

func inspectExecutable(ctx context.Context, name string, candidates []string, versionArgs ...string) DependencyInfo {
	info := DependencyInfo{Name: name, Status: AvailabilityMissing}
	path, err := findExecutable(candidates...)
	if err != nil {
		return info
	}
	info.Path = path

	out, err := exec.CommandContext(ctx, path, versionArgs...).CombinedOutput()
	version := strings.TrimSpace(string(out))
	if line, _, ok := strings.Cut(version, "\n"); ok {
		version = strings.TrimSpace(line)
	}
	info.Version = version
	if err != nil {
		info.Status = AvailabilityInvalid
		return info
	}
	info.Status = AvailabilityAvailable
	return info
}

func aggregateRuntimeInfo(runtimeName string, primary DependencyInfo, required, optional []DependencyInfo) RuntimeInfo {
	status := primary.Status
	if status == "" {
		status = AvailabilityInvalid
	}
	if status == AvailabilityAvailable {
		for _, dependency := range required {
			if dependency.Status != AvailabilityAvailable {
				status = AvailabilityInvalid
				break
			}
		}
	}
	dependencies := make([]DependencyInfo, 0, len(required)+len(optional))
	dependencies = append(dependencies, required...)
	dependencies = append(dependencies, optional...)
	return RuntimeInfo{
		Runtime:      runtimeName,
		Status:       status,
		Version:      primary.Version,
		Dependencies: dependencies,
	}
}

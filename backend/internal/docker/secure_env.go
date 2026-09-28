package docker

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func secureEnvFile(values map[string]string) (string, func(), error) {
	if len(values) == 0 {
		return "", func() {}, nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	file, err := os.CreateTemp("", "devbox-docker-env-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create Docker environment file: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("secure Docker environment file: %w", err)
	}
	for _, key := range keys {
		if err := validateEnvironmentName(key); err != nil {
			_ = file.Close()
			cleanup()
			return "", func() {}, err
		}
		value := values[key]
		if err := validateValue(value, "environment value"); err != nil {
			_ = file.Close()
			cleanup()
			return "", func() {}, err
		}
		if strings.Contains(value, "\n") || strings.Contains(value, "\r") {
			_ = file.Close()
			cleanup()
			return "", func() {}, fmt.Errorf("%w: environment value contains a newline", ErrInvalidInput)
		}
		if _, err := file.WriteString(key + "=" + value + "\n"); err != nil {
			_ = file.Close()
			cleanup()
			return "", func() {}, fmt.Errorf("write Docker environment file: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close Docker environment file: %w", err)
	}
	return path, cleanup, nil
}

func containerPortBindingArg(binding providers.ContainerPortBinding) (string, error) {
	if binding.HostPort < 1 || binding.HostPort > 65535 || binding.ContainerPort < 1 || binding.ContainerPort > 65535 {
		return "", fmt.Errorf("%w: port out of range", ErrInvalidInput)
	}
	host := strings.TrimSpace(binding.HostIP)
	if host == "" {
		return strconv.Itoa(binding.HostPort) + ":" + strconv.Itoa(binding.ContainerPort), nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("%w: host IP for Docker publication is invalid", ErrInvalidInput)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(binding.HostPort) + ":" + strconv.Itoa(binding.ContainerPort), nil
}

func validateRestartPolicy(policy string) error {
	switch strings.TrimSpace(policy) {
	case "", "no", "always", "unless-stopped", "on-failure":
		return nil
	default:
		return fmt.Errorf("%w: unsupported restart policy", ErrInvalidInput)
	}
}

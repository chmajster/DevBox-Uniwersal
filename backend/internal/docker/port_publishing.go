package docker

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func (p *CLIProvider) ReplaceManaged(ctx context.Context, spec containerspec.DeploymentSpec) error {
	return p.ReplaceManagedPorts(ctx, spec, nil)
}

// Validate all requested bindings before the old container is stopped.
func managedPublishArgs(spec containerspec.DeploymentSpec, additional []providers.PublishedPort) ([]string, error) {
	ports := append([]providers.PublishedPort{{HostPort: spec.HostPort, ContainerPort: spec.ContainerPort}}, additional...)
	if err := validatePublishedPorts(ports); err != nil {
		return nil, err
	}
	args := make([]string, 0, 2*len(ports))
	for _, port := range ports {
		args = append(args, "--publish", strconv.Itoa(port.HostPort)+":"+strconv.Itoa(port.ContainerPort))
	}
	return args, nil
}

func validatePublishedPorts(ports []providers.PublishedPort) error {
	if len(ports) < 1 || len(ports) > 16 {
		return fmt.Errorf("%w: expected between 1 and 16 published ports", ErrInvalidInput)
	}
	seen := make(map[int]bool, len(ports))
	for _, port := range ports {
		if port.HostPort < 1 || port.HostPort > 65535 || port.ContainerPort < 1 || port.ContainerPort > 65535 {
			return fmt.Errorf("%w: published and container ports must be between 1 and 65535", ErrInvalidInput)
		}
		if seen[port.HostPort] {
			return fmt.Errorf("%w: duplicate published port %d", ErrInvalidInput, port.HostPort)
		}
		seen[port.HostPort] = true
	}
	return nil
}

// CheckPublishedHTTP probes only the local published endpoint, never a redirect
// target or an environment-configured HTTP proxy. HTTPS publishing is TLS
// passthrough and is not a certificate provisioning/validation service.
func (p *CLIProvider) CheckPublishedHTTP(ctx context.Context, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: invalid healthcheck port", ErrInvalidInput)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 2 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	target := "http://127.0.0.1:" + strconv.Itoa(port) + "/"
	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode < 500 {
				return nil
			}
			err = fmt.Errorf("HTTP status %d", response.StatusCode)
		}
		lastErr = err
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("published HTTP port %d did not become ready: %v: %w", port, lastErr, ctx.Err())
		case <-timer.C:
		}
	}
}

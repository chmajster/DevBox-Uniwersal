package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func (p *CLIProvider) EnsureNetwork(ctx context.Context, name string) error {
	if err := validateNetworkRef(name); err != nil {
		return err
	}
	if _, err := p.InspectNetwork(ctx, name); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if _, stderr, err := p.runner.Run(ctx, "network", "create", name); err != nil {
		if strings.Contains(strings.ToLower(string(stderr)), "already exists") {
			return nil
		}
		if _, inspectErr := p.InspectNetwork(ctx, name); inspectErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (p *CLIProvider) EnsureVolume(ctx context.Context, name string) error {
	if err := validateVolumeRef(name); err != nil {
		return err
	}
	if _, err := p.InspectVolume(ctx, name); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if _, stderr, err := p.runner.Run(ctx, "volume", "create", name); err != nil {
		if strings.Contains(strings.ToLower(string(stderr)), "already exists") {
			return nil
		}
		if _, inspectErr := p.InspectVolume(ctx, name); inspectErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (p *CLIProvider) ConnectNetwork(ctx context.Context, container, network string) error {
	if err := validateContainerRef(container); err != nil {
		return err
	}
	if err := validateNetworkRef(network); err != nil {
		return err
	}
	_, stderr, err := p.runner.Run(ctx, "network", "connect", network, container)
	if err == nil || strings.Contains(strings.ToLower(string(stderr)), "already exists") {
		return nil
	}
	if !networkNotFound(stderr, err) {
		return err
	}

	// A shared network can disappear between reconciliation and the first
	// attachment (for example because of an external prune). Recreate it and
	// retry the connect exactly once instead of failing the whole deployment.
	if ensureErr := p.EnsureNetwork(ctx, network); ensureErr != nil {
		return fmt.Errorf("recreate Docker network %s: %w", network, ensureErr)
	}
	_, retryStderr, retryErr := p.runner.Run(ctx, "network", "connect", network, container)
	if retryErr != nil && strings.Contains(strings.ToLower(string(retryStderr)), "already exists") {
		return nil
	}
	return retryErr
}

func networkNotFound(stderr []byte, err error) bool {
	lower := strings.ToLower(strings.TrimSpace(string(stderr)))
	if !errors.Is(err, ErrNotFound) && !strings.Contains(lower, "not found") {
		return false
	}
	return strings.Contains(lower, "no such network") ||
		(strings.Contains(lower, "network ") && strings.Contains(lower, " not found"))
}

func (p *CLIProvider) EnsureImage(ctx context.Context, image string) error {
	if err := validateImageRef(image); err != nil {
		return err
	}
	if _, err := p.InspectImage(ctx, image); err == nil {
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return p.PullImage(ctx, image)
}

func (p *CLIProvider) EnsureContainer(ctx context.Context, spec providers.ContainerSpec) (providers.ContainerInfo, error) {
	if err := validateContainerRef(spec.Name); err != nil {
		return providers.ContainerInfo{}, err
	}
	item, err := p.InspectContainer(ctx, spec.Name)
	if err == nil {
		if item.Image == spec.Image && containerLabelsMatch(item.Labels, spec.Labels) {
			return providers.ContainerInfo{ID: item.ID, Name: item.Name, State: item.State}, nil
		}
		if item.Running {
			if err := p.Stop(ctx, spec.Name); err != nil {
				return providers.ContainerInfo{}, err
			}
		}
		if err := p.Remove(ctx, spec.Name); err != nil {
			return providers.ContainerInfo{}, err
		}
		return p.Create(ctx, spec)
	}
	if !errors.Is(err, ErrNotFound) {
		return providers.ContainerInfo{}, err
	}
	return p.Create(ctx, spec)
}

func containerLabelsMatch(actual, desired map[string]string) bool {
	for key, value := range desired {
		if actual[key] != value {
			return false
		}
	}
	return true
}

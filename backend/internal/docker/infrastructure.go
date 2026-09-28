package docker

import (
	"context"
	"errors"
	"strings"
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
	if err != nil && strings.Contains(strings.ToLower(string(stderr)), "already exists") {
		return nil
	}
	return err
}

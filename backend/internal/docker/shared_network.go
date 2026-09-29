package docker

import (
	"context"
	"fmt"
	"strings"
)

func (p *CLIProvider) ConnectComposeProjectNetwork(ctx context.Context, directory, projectName, network string) error {
	if err := validateProjectName(projectName); err != nil {
		return err
	}
	if err := validateNetworkRef(network); err != nil {
		return err
	}
	if err := p.EnsureNetwork(ctx, network); err != nil {
		return err
	}
	args, err := composeArgs(directory, projectName)
	if err != nil {
		return err
	}
	args = append(args, "ps", "-q")
	out, _, err := p.runCompose(ctx, args...)
	if err != nil {
		return fmt.Errorf("list Compose containers for shared network: %w", err)
	}
	for _, id := range strings.Fields(string(out)) {
		if err := p.ConnectNetwork(ctx, id, network); err != nil {
			return fmt.Errorf("connect Compose container %s to network %s: %w", id, network, err)
		}
	}
	return nil
}

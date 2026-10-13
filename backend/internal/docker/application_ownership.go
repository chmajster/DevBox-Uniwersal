package docker

import (
	"context"
	"encoding/json"
	"fmt"
)

func (p *CLIProvider) VerifyApplicationContainer(ctx context.Context, container, applicationID string) error {
	if err := validateContainerRef(container); err != nil {
		return err
	}
	out, _, err := p.runner.Run(ctx, "container", "inspect", "--format", "{{json .Config.Labels}}", container)
	if err != nil {
		return err
	}
	var labels map[string]string
	if json.Unmarshal(out, &labels) != nil || labels["io.devbox.application.id"] != applicationID {
		return fmt.Errorf("refusing to modify a container owned by another application")
	}
	return nil
}

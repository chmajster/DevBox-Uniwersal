package projects

import (
	"context"
	"fmt"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

const httpPortPurpose = "application"
const httpsPortPurpose = "application-https"

type managedPortPublisher interface {
	ReplaceManagedPorts(context.Context, containerspec.DeploymentSpec, []providers.PublishedPort) error
}

type deploymentPortPlan struct {
	repo      *Repository
	allocator providers.PortAllocator
	projectID string
	requested PortSettings
	state     AppliedPortSettings
	newLeases []providers.PortLease
	oldLeases []providers.PortLease
	activated bool
}

func (h *DeploymentHandler) preparePortPlan(ctx context.Context, project Project, config PortConfiguration, containerPort int) (*deploymentPortPlan, error) {
	if err := ValidatePortSettings(config.Settings); err != nil {
		return nil, err
	}
	if containerPort < 1 || containerPort > 65535 {
		return nil, fmt.Errorf("cannot detect the application listener; set its internal Docker port in project configuration")
	}
	if config.Settings.HTTPSEnabled && containerPort == config.Settings.HTTPSContainerPort {
		return nil, fmt.Errorf("HTTP and HTTPS require different container listeners")
	}
	if h.integrations.Ports == nil {
		return nil, fmt.Errorf("%w: port allocator", ErrProviderUnavailable)
	}
	plan := &deploymentPortPlan{repo: h.repo, allocator: h.integrations.Ports, projectID: project.ID, requested: config.Settings, state: AppliedPortSettings{Settings: config.Settings}}
	previousHTTP, previousHTTPS := 0, 0
	if config.Applied != nil {
		old := config.Applied
		plan.oldLeases = append(plan.oldLeases, providers.PortLease{Port: old.HTTP.HostPort, ProjectID: project.ID, Purpose: httpPortPurpose})
		if old.Settings.HostPort == config.Settings.HostPort || old.HTTP.HostPort == config.Settings.HostPort {
			previousHTTP = old.HTTP.HostPort
		}
		if old.HTTPS != nil {
			plan.oldLeases = append(plan.oldLeases, providers.PortLease{Port: old.HTTPS.HostPort, ProjectID: project.ID, Purpose: httpsPortPurpose})
			if old.Settings.HTTPSHostPort == config.Settings.HTTPSHostPort || old.HTTPS.HostPort == config.Settings.HTTPSHostPort {
				previousHTTPS = old.HTTPS.HostPort
			}
		}
	} else if project.Port != nil {
		plan.oldLeases = append(plan.oldLeases, providers.PortLease{Port: *project.Port, ProjectID: project.ID, Purpose: httpPortPurpose})
		if *project.Port == config.Settings.HostPort {
			previousHTTP = *project.Port
		}
	}
	hostPort, err := plan.reserve(ctx, httpPortPurpose, config.Settings.HostPort, previousHTTP)
	if err != nil {
		return nil, fmt.Errorf("allocate HTTP port: %w", err)
	}
	plan.state.HTTP = providers.PublishedPort{HostPort: hostPort, ContainerPort: containerPort}
	if config.Settings.HTTPSEnabled {
		hostPort, err := plan.reserve(ctx, httpsPortPurpose, config.Settings.HTTPSHostPort, previousHTTPS)
		if err != nil {
			plan.rollback()
			return nil, fmt.Errorf("allocate HTTPS port: %w", err)
		}
		if hostPort == plan.state.HTTP.HostPort {
			plan.rollback()
			return nil, fmt.Errorf("port allocator returned the same host port for HTTP and HTTPS")
		}
		plan.state.HTTPS = &providers.PublishedPort{HostPort: hostPort, ContainerPort: config.Settings.HTTPSContainerPort}
	}
	return plan, nil
}

func (p *deploymentPortPlan) reserve(ctx context.Context, purpose string, requested, previous int) (int, error) {
	if previous > 0 {
		if owner, ok := p.allocator.(providers.PortLeaseOwner); ok {
			owned, err := owner.Owns(ctx, p.projectID, purpose, previous)
			if err != nil {
				return 0, err
			}
			if owned {
				// The existing container normally occupies this socket. An owned
				// live lease is reused instead of treating it as a collision.
				return previous, nil
			}
		}
	}
	var lease providers.PortLease
	var err error
	reused := false
	if sequential, ok := p.allocator.(providers.SequentialPortAllocator); ok {
		reservation, reserveErr := sequential.ReserveFromOwned(ctx, p.projectID, purpose, requested)
		lease, err, reused = reservation.PortLease, reserveErr, reservation.Reused
	} else {
		// Third-party allocators keep their exact-port semantics. DevBox's
		// concrete PortManager implements the collision-aware extension.
		lease, err = p.allocator.Reserve(ctx, p.projectID, purpose, &requested)
	}
	if err != nil {
		return 0, err
	}
	if !reused {
		p.newLeases = append(p.newLeases, lease)
	}
	return lease.Port, nil
}

func (p *deploymentPortPlan) bindings() []providers.PublishedPort {
	ports := []providers.PublishedPort{p.state.HTTP}
	if p.state.HTTPS != nil {
		ports = append(ports, *p.state.HTTPS)
	}
	return ports
}

func (p *deploymentPortPlan) release(ctx context.Context, lease providers.PortLease) error {
	if owner, ok := p.allocator.(providers.PortLeaseOwner); ok {
		return owner.ReleaseOwned(ctx, p.projectID, lease.Purpose, lease.Port)
	}
	return p.allocator.Release(ctx, lease.Port)
}

func (p *deploymentPortPlan) rollback() {
	if p == nil || p.activated {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, lease := range p.newLeases {
		_ = p.release(ctx, lease)
	}
}

func (p *deploymentPortPlan) commit() error {
	// Do not release a port that is already serving the replacement container,
	// even if a later database or reverse-proxy operation fails.
	p.activated = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.state.AppliedAt = time.Now().UTC()
	// Persist the resolved numbers as the editable per-project settings. A
	// subsequent explicit edit back to 8080 is then distinguishable from an
	// ordinary redeploy that must keep the assigned 8081.
	p.state.Settings.HostPort = p.state.HTTP.HostPort
	if p.state.HTTPS != nil {
		p.state.Settings.HTTPSHostPort = p.state.HTTPS.HostPort
	}
	if err := p.repo.saveAppliedPorts(ctx, p.projectID, p.state, p.requested); err != nil {
		return fmt.Errorf("container is running, but persisting its port mapping failed: %w", err)
	}
	for _, lease := range p.oldLeases {
		used := lease.Port == p.state.HTTP.HostPort
		if p.state.HTTPS != nil && lease.Port == p.state.HTTPS.HostPort {
			used = true
		}
		if !used {
			if err := p.release(ctx, lease); err != nil {
				return fmt.Errorf("container is running, but releasing old port %d failed: %w", lease.Port, err)
			}
		}
	}
	return nil
}

func (h *DeploymentHandler) replaceWithPortPlan(ctx context.Context, spec containerspec.DeploymentSpec, plan *deploymentPortPlan) error {
	if plan.state.HTTPS == nil {
		return h.integrations.Managed.ReplaceManaged(ctx, spec)
	}
	publisher, ok := h.integrations.Managed.(managedPortPublisher)
	if !ok {
		return fmt.Errorf("%w: managed container multi-port publishing", ErrProviderUnavailable)
	}
	return publisher.ReplaceManagedPorts(ctx, spec, []providers.PublishedPort{*plan.state.HTTPS})
}

func (h *DeploymentHandler) logPortPlan(ctx context.Context, jobID, phase string, plan *deploymentPortPlan) {
	fields := map[string]any{"http": plan.state.HTTP, "requested_http_port": plan.requested.HostPort}
	level := "info"
	if plan.state.HTTP.HostPort != plan.requested.HostPort {
		level = "warn"
	}
	if plan.state.HTTPS != nil {
		fields["https"] = plan.state.HTTPS
		fields["requested_https_port"] = plan.requested.HTTPSHostPort
		if plan.state.HTTPS.HostPort != plan.requested.HTTPSHostPort {
			level = "warn"
		}
	}
	_ = h.logger.Log(ctx, jobID, level, "deployment.ports."+phase, fields)
}

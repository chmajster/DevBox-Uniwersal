package proxy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type Service struct {
	repo          *SQLiteRepository
	nginx         *NginxProvider
	hosts         HostsManager
	health        *HealthChecker
	healthTimeout time.Duration
}

func NewService(repo *SQLiteRepository, nginx *NginxProvider, hosts HostsManager, health *HealthChecker, healthTimeout time.Duration) *Service {
	return &Service{repo: repo, nginx: nginx, hosts: hosts, health: health, healthTimeout: healthTimeout}
}

func (s *Service) ListDomains(ctx context.Context) ([]Domain, error) {
	return s.repo.ListDomains(ctx)
}

func (s *Service) CreateDomain(ctx context.Context, projectID, hostname string, targetPort int) (DomainMutationResult, error) {
	if projectID == "" {
		return DomainMutationResult{}, fmt.Errorf("%w: project_id is required", ErrInvalidInput)
	}
	normalized, err := NormalizeHostname(hostname)
	if err != nil {
		return DomainMutationResult{}, err
	}
	if targetPort < 1 || targetPort > 65535 {
		return DomainMutationResult{}, fmt.Errorf("%w: target_port must be between 1 and 65535", ErrInvalidInput)
	}
	if _, err := s.repo.DomainByHostname(ctx, normalized); err == nil {
		return DomainMutationResult{}, fmt.Errorf("%w: hostname already exists", ErrConflict)
	} else if !errors.Is(err, ErrNotFound) {
		return DomainMutationResult{}, err
	}

	route := routeFor(normalized, targetPort)
	if err := s.nginx.CreateSite(ctx, route); err != nil {
		return DomainMutationResult{}, err
	}
	domain, err := s.repo.CreateDomain(ctx, projectID, normalized, targetPort)
	if err != nil {
		_ = s.nginx.DeleteSite(ctx, normalized)
		return DomainMutationResult{}, err
	}
	if _, err := s.health.RunAndStore(ctx, projectID, "http", healthTarget(targetPort), s.healthTimeout); err != nil {
		_ = s.repo.DeleteDomain(ctx, domain.ID)
		_ = s.nginx.DeleteSite(ctx, normalized)
		return DomainMutationResult{}, err
	}
	hostsResult, err := s.hosts.Ensure(ctx, normalized, "127.0.0.1")
	if err != nil {
		return DomainMutationResult{}, err
	}
	domain, err = s.repo.DomainByID(ctx, domain.ID)
	if err != nil {
		return DomainMutationResult{}, err
	}
	return DomainMutationResult{Domain: domain, Hosts: hostsResult}, nil
}

func (s *Service) UpdateDomain(ctx context.Context, id string, hostname *string, targetPort *int) (DomainMutationResult, error) {
	current, err := s.repo.DomainByID(ctx, id)
	if err != nil {
		return DomainMutationResult{}, err
	}
	nextHostname := current.Hostname
	nextPort := current.TargetPort
	if hostname != nil {
		nextHostname, err = NormalizeHostname(*hostname)
		if err != nil {
			return DomainMutationResult{}, err
		}
	}
	if targetPort != nil {
		if *targetPort < 1 || *targetPort > 65535 {
			return DomainMutationResult{}, fmt.Errorf("%w: target_port must be between 1 and 65535", ErrInvalidInput)
		}
		nextPort = *targetPort
	}
	if nextHostname != current.Hostname {
		if existing, lookupErr := s.repo.DomainByHostname(ctx, nextHostname); lookupErr == nil && existing.ID != id {
			return DomainMutationResult{}, fmt.Errorf("%w: hostname already exists", ErrConflict)
		} else if lookupErr != nil && !errors.Is(lookupErr, ErrNotFound) {
			return DomainMutationResult{}, lookupErr
		}
	}

	oldRoute := routeFor(current.Hostname, current.TargetPort)
	newRoute := routeFor(nextHostname, nextPort)
	if err := s.nginx.UpdateSite(ctx, newRoute); err != nil {
		return DomainMutationResult{}, err
	}
	hostnameChanged := nextHostname != current.Hostname
	if hostnameChanged {
		if err := s.nginx.DeleteSite(ctx, current.Hostname); err != nil {
			_ = s.nginx.DeleteSite(ctx, nextHostname)
			_ = s.nginx.Apply(ctx, oldRoute)
			return DomainMutationResult{}, err
		}
	}
	updated, err := s.repo.UpdateDomain(ctx, id, nextHostname, nextPort)
	if err != nil {
		if hostnameChanged {
			_ = s.nginx.DeleteSite(ctx, nextHostname)
		}
		_ = s.nginx.Apply(ctx, oldRoute)
		return DomainMutationResult{}, err
	}
	if _, err := s.health.RunAndStore(ctx, updated.ProjectID, "http", healthTarget(updated.TargetPort), s.healthTimeout); err != nil {
		return DomainMutationResult{}, err
	}

	hostsResult, err := s.hosts.Ensure(ctx, updated.Hostname, "127.0.0.1")
	if err != nil {
		return DomainMutationResult{}, err
	}
	if hostnameChanged {
		_, _ = s.hosts.Remove(ctx, current.Hostname)
	}
	updated, err = s.repo.DomainByID(ctx, id)
	if err != nil {
		return DomainMutationResult{}, err
	}
	return DomainMutationResult{Domain: updated, Hosts: hostsResult}, nil
}

func (s *Service) DeleteDomain(ctx context.Context, id string) (Domain, HostChange, error) {
	current, err := s.repo.DomainByID(ctx, id)
	if err != nil {
		return Domain{}, HostChange{}, err
	}
	if err := s.nginx.DeleteSite(ctx, current.Hostname); err != nil {
		return Domain{}, HostChange{}, err
	}
	if err := s.repo.DeleteDomain(ctx, id); err != nil {
		_ = s.nginx.Apply(ctx, routeFor(current.Hostname, current.TargetPort))
		return Domain{}, HostChange{}, err
	}
	hostsResult, err := s.hosts.Remove(ctx, current.Hostname)
	if err != nil {
		return Domain{}, HostChange{}, err
	}
	return current, hostsResult, nil
}

func (s *Service) TestRoute(ctx context.Context, hostname string, targetPort int) error {
	normalized, err := NormalizeHostname(hostname)
	if err != nil {
		return err
	}
	if targetPort < 1 || targetPort > 65535 {
		return fmt.Errorf("%w: target_port must be between 1 and 65535", ErrInvalidInput)
	}
	return s.nginx.TestRoute(ctx, routeFor(normalized, targetPort))
}

func (s *Service) ProxyStatus(ctx context.Context) ProxyStatus {
	status := ProxyStatus{}
	detected, err := s.nginx.Detect(ctx)
	status.Detected = detected
	if err != nil {
		status.Error = err.Error()
		return status
	}
	if !detected {
		status.Error = "nginx executable not found"
		return status
	}
	version, err := s.nginx.Version(ctx)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Version = version
	if err := s.nginx.Validate(ctx); err != nil {
		status.Error = err.Error()
		return status
	}
	status.ConfigValid = true
	return status
}

func routeFor(hostname string, port int) providers.ProxyRoute {
	return providers.ProxyRoute{
		Domain:   hostname,
		Upstream: fmt.Sprintf("http://127.0.0.1:%d", port),
		TLS:      false,
	}
}

func healthTarget(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", port)
}

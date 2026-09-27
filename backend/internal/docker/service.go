package docker

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
)

type Service struct {
	provider     *CLIProvider
	audit        *audit.Service
	projectsRoot string
}

func NewService(provider *CLIProvider, auditService *audit.Service, projectsRoot string) *Service {
	if projectsRoot == "" {
		projectsRoot = "./projects"
	}
	return &Service{provider: provider, audit: auditService, projectsRoot: projectsRoot}
}

func (s *Service) Status(ctx context.Context) Status {
	status, err := s.provider.Status(ctx)
	if err != nil {
		status.Available = false
		if status.Error == "" {
			status.Error = err.Error()
		}
	}
	return status
}

func (s *Service) Containers(ctx context.Context) ([]Container, error) {
	return s.provider.ListContainers(ctx)
}

func (s *Service) Container(ctx context.Context, id string) (ContainerDetail, error) {
	return s.provider.InspectContainer(ctx, id)
}

func (s *Service) ContainerAction(ctx context.Context, actorID, remoteAddr, id, action string) error {
	var err error
	switch action {
	case "start":
		err = s.provider.Start(ctx, id)
	case "stop":
		err = s.provider.Stop(ctx, id)
	case "restart":
		err = s.provider.Restart(ctx, id)
	case "remove":
		err = s.provider.Remove(ctx, id)
	default:
		return fmt.Errorf("%w: unsupported container action", ErrInvalidInput)
	}
	if err != nil {
		return err
	}
	if s.audit != nil {
		resourceID := id
		actor := actorID
		remote := remoteAddr
		if err := s.audit.Record(ctx, &actor, "docker.container."+action, "docker_container", &resourceID, nil, &remote); err != nil {
			return fmt.Errorf("container action succeeded but audit write failed: %w", err)
		}
	}
	return nil
}

func (s *Service) ContainerLogs(ctx context.Context, id string, tail int) (io.ReadCloser, error) {
	return s.provider.Logs(ctx, id, tail, false)
}

func (s *Service) ContainerExec(ctx context.Context, actorID, remoteAddr, id string, command ExecCommand) (string, error) {
	out, err := s.provider.Exec(ctx, id, command)
	if err != nil {
		return "", err
	}
	if s.audit != nil {
		resourceID := id
		actor := actorID
		remote := remoteAddr
		if err := s.audit.Record(ctx, &actor, "docker.container.exec", "docker_container", &resourceID, map[string]any{"command": command}, &remote); err != nil {
			return "", fmt.Errorf("container exec succeeded but audit write failed: %w", err)
		}
	}
	return out, nil
}

func (s *Service) Images(ctx context.Context) ([]Image, error) {
	return s.provider.ListImages(ctx)
}

func (s *Service) Volumes(ctx context.Context) ([]Volume, error) {
	return s.provider.ListVolumes(ctx)
}

func (s *Service) Networks(ctx context.Context) ([]Network, error) {
	return s.provider.ListNetworks(ctx)
}

func (s *Service) ComposeProjects() ([]ComposeProject, error) {
	return DiscoverComposeProjects(s.projectsRoot)
}

func (s *Service) composeTarget(name string) (string, string, error) {
	dir, err := safeChild(s.projectsRoot, name)
	if err != nil {
		return "", "", err
	}
	composeName, err := composeProjectName(filepath.Base(dir))
	if err != nil {
		return "", "", err
	}
	if _, err := findComposeFile(dir); err != nil {
		return "", "", err
	}
	return dir, composeName, nil
}

func (s *Service) ComposePS(ctx context.Context, name string) ([]ComposeProcess, error) {
	dir, projectName, err := s.composeTarget(name)
	if err != nil {
		return nil, err
	}
	return s.provider.ComposePS(ctx, dir, projectName)
}

func (s *Service) ComposeLogs(ctx context.Context, name, service string, tail int) (io.ReadCloser, error) {
	dir, projectName, err := s.composeTarget(name)
	if err != nil {
		return nil, err
	}
	return s.provider.ComposeLogs(ctx, dir, projectName, service, tail, false)
}

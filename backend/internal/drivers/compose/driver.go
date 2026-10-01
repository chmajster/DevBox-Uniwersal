package compose

import (
	"context"
	"fmt"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	dockerapi "github.com/chmajster/DevBox-Uniwersal/backend/internal/docker"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/driverutil"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type engine interface {
	Available(context.Context) error
	ComposeValidate(context.Context, string, string) error
	ComposePull(context.Context, string, string, string) error
	ComposeBuild(context.Context, string, string, string) error
	ComposeUpApplication(context.Context, string, string, map[string]map[string]string) error
	ComposeHealthy(context.Context, string, string) error
	ComposePS(context.Context, string, string) ([]dockerapi.ComposeProcess, error)
	ComposeStart(context.Context, string, string) error
	ComposeStop(context.Context, string, string) error
	ComposeRestart(context.Context, string, string, string) error
	ComposeDown(context.Context, string, string) error
	ComposeApplicationServices(context.Context, string, string) ([]dockerapi.ComposeApplicationService, error)
	DiscoverComposeApplicationPorts(context.Context, string, string, string) (providers.ComposePortDiscovery, error)
	ConfigureComposePorts(context.Context, string, string, string, []providers.PublishedPort) (func() error, error)
}
type networkProvider interface {
	EnsureNetwork(context.Context, string) error
	ConnectComposeProjectNetwork(context.Context, string, string, string) error
}

type Driver struct {
	engine        engine
	ports         applications.PortAllocator
	networks      networkProvider
	sharedNetwork string
}

func New(engine engine, ports applications.PortAllocator, networks networkProvider, sharedNetwork string) *Driver {
	return &Driver{engine: engine, ports: ports, networks: networks, sharedNetwork: sharedNetwork}
}
func (d *Driver) Name() string { return "compose" }

func (d *Driver) Detect(ctx context.Context, request applications.DetectRequest) (applications.DetectionResult, error) {
	if request.WorkDir == "" {
		return applications.DetectionResult{}, applications.ErrConfigurationRequired
	}
	projectName := composeProjectName(request)
	services, err := d.engine.ComposeApplicationServices(ctx, request.WorkDir, projectName)
	if err != nil {
		return applications.DetectionResult{}, err
	}
	discovery, err := d.engine.DiscoverComposeApplicationPorts(ctx, request.WorkDir, projectName, "")
	if err != nil {
		return applications.DetectionResult{}, err
	}
	manifest, err := applications.LoadManifest(request.WorkDir)
	if err != nil {
		return applications.DetectionResult{}, err
	}
	result := applications.DetectionResult{Driver: d.Name(), Confidence: "high", Reasons: []string{"Compose file normalized successfully"}}
	primaryService := ""
	for _, service := range services {
		role, primary := service.Role, service.Primary
		if manifest != nil {
			for _, override := range manifest.Workloads {
				target := override.Service
				if target == "" {
					target = service.Name
				}
				if target == service.Name {
					if override.Role != "" {
						role = override.Role
					}
					if override.Primary {
						primary = true
					}
				}
			}
		}
		if primary {
			if primaryService != "" && primaryService != service.Name {
				result.Warnings = append(result.Warnings, "multiple workloads declare primary=true")
			} else {
				primaryService = service.Name
			}
		}
		confidence := "medium"
		reason := "Compose service name/image/command/ports classified"
		if explicit := service.Labels["io.devbox.role"]; explicit != "" {
			confidence = "high"
			reason = "io.devbox.role label"
		}
		result.Services = append(result.Services, applications.ServiceDetection{Name: service.Name, SuggestedRole: role, Primary: primary, Confidence: confidence, Reason: reason})
	}
	if manifest != nil && len(manifest.Endpoints) > 0 {
		for name, endpoint := range manifest.Endpoints {
			result.Endpoints = append(result.Endpoints, applications.EndpointDetection{Service: endpoint.Workload, Protocol: defaultProtocol(endpoint.Protocol), ContainerPort: endpoint.ContainerPort, Primary: endpoint.Primary, Confidence: "high", Reason: "devbox.yaml endpoint " + name})
		}
	} else {
		for _, candidate := range discovery.Candidates {
			primary := discovery.Selected != nil && candidate.Service == discovery.Selected.Service && candidate.ContainerPort == discovery.Selected.ContainerPort
			if primaryService != "" {
				primary = candidate.Service == primaryService
			}
			result.Endpoints = append(result.Endpoints, applications.EndpointDetection{Service: candidate.Service, Protocol: candidate.Protocol, ContainerPort: candidate.ContainerPort, Primary: primary, Confidence: "medium", Reason: candidate.Source})
		}
	}
	primaryCount := 0
	for _, endpoint := range result.Endpoints {
		if endpoint.Primary {
			primaryCount++
		}
	}
	if len(result.Endpoints) > 1 && primaryCount != 1 {
		result.RequiresConfiguration = true
		result.Warnings = append(result.Warnings, "multiple web/API endpoints are equally plausible; choose the primary endpoint")
	}
	if primaryCount > 1 {
		result.RequiresConfiguration = true
		result.Warnings = append(result.Warnings, "more than one primary endpoint is configured")
	}
	return result, nil
}

func (d *Driver) Plan(ctx context.Context, request applications.PlanRequest) (applications.DeploymentPlan, error) {
	projectName := request.Application.Slug
	services, err := d.engine.ComposeApplicationServices(ctx, request.WorkDir, projectName)
	if err != nil {
		return applications.DeploymentPlan{}, err
	}
	discovery, err := d.engine.DiscoverComposeApplicationPorts(ctx, request.WorkDir, projectName, "")
	if err != nil {
		return applications.DeploymentPlan{}, err
	}
	manifest, err := applications.LoadManifest(request.WorkDir)
	if err != nil {
		return applications.DeploymentPlan{}, err
	}
	plan := applications.DeploymentPlan{Version: 1, ApplicationID: request.Application.ID, Driver: d.Name(), SourceRevision: request.SourceRevision, Networks: nonEmpty(d.sharedNetwork), Metadata: map[string]any{"compose_project": projectName, "requires_configuration": request.Detection.RequiresConfiguration}}
	primaryService := ""
	for _, service := range services {
		role, primary := service.Role, service.Primary
		if manifest != nil {
			for _, override := range manifest.Workloads {
				target := override.Service
				if target == "" {
					target = service.Name
				}
				if target == service.Name {
					if override.Role != "" {
						role = override.Role
					}
					if override.Primary {
						primary = true
					}
				}
			}
		}
		if primary {
			primaryService = service.Name
		}
		plan.Workloads = append(plan.Workloads, applications.PlannedWorkload{Name: service.Name, Role: role, Image: service.Image, Primary: primary})
	}
	if manifest != nil && len(manifest.Endpoints) > 0 {
		for name, item := range manifest.Endpoints {
			plan.Endpoints = append(plan.Endpoints, applications.PlannedEndpoint{Name: name, Workload: item.Workload, Protocol: defaultProtocol(item.Protocol), ContainerPort: item.ContainerPort, Public: item.Public, Primary: item.Primary, HealthPath: item.HealthPath})
		}
	} else {
		for _, candidate := range discovery.Candidates {
			primary := discovery.Selected != nil && candidate.Service == discovery.Selected.Service && candidate.ContainerPort == discovery.Selected.ContainerPort
			if primaryService != "" {
				primary = candidate.Service == primaryService
			}
			name := candidate.Service
			if countService(discovery.Candidates, candidate.Service) > 1 {
				name = fmt.Sprintf("%s-%d", candidate.Service, candidate.ContainerPort)
			}
			plan.Endpoints = append(plan.Endpoints, applications.PlannedEndpoint{Name: name, Workload: candidate.Service, Protocol: defaultProtocol(candidate.Protocol), ContainerPort: candidate.ContainerPort, HostPort: candidate.HostPort, Public: primary, Primary: primary})
		}
	}
	for _, endpoint := range plan.Endpoints {
		if endpoint.Primary {
			kind := "tcp"
			if endpoint.Protocol == "http" || endpoint.Protocol == "https" {
				kind = "http"
			}
			plan.Healthchecks = append(plan.Healthchecks, applications.HealthCheckPlan{Workload: endpoint.Workload, Type: kind, Path: endpoint.HealthPath, Port: endpoint.ContainerPort})
		}
	}
	return plan, nil
}

func (d *Driver) Deploy(ctx context.Context, request applications.ExecutionRequest, plan applications.DeploymentPlan) (applications.DeploymentResult, error) {
	if required, _ := plan.Metadata["requires_configuration"].(bool); required {
		return applications.DeploymentResult{}, applications.ErrConfigurationRequired
	}
	if err := d.engine.Available(ctx); err != nil {
		return applications.DeploymentResult{}, fmt.Errorf("%w: docker: %v", applications.ErrProviderUnavailable, err)
	}
	projectName := request.Application.Slug
	if err := d.engine.ComposeValidate(ctx, request.WorkDir, projectName); err != nil {
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StagePrepare, Driver: d.Name(), Operation: "compose_validate", Reason: err.Error(), Action: "fix compose.yaml before deploying"}
	}
	if err := d.engine.ComposePull(ctx, request.WorkDir, projectName, ""); err != nil {
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageDependencies, Driver: d.Name(), Operation: "compose_pull", Reason: err.Error(), Action: "verify registry access and image references"}
	}
	if err := d.engine.ComposeBuild(ctx, request.WorkDir, projectName, ""); err != nil {
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageBuildOrPull, Driver: d.Name(), Operation: "compose_build", Reason: err.Error(), Action: "inspect service build contexts and Dockerfiles"}
	}
	if d.sharedNetwork != "" {
		if d.networks == nil {
			return applications.DeploymentResult{}, fmt.Errorf("%w: network provider", applications.ErrProviderUnavailable)
		}
		if err := d.networks.EnsureNetwork(ctx, d.sharedNetwork); err != nil {
			return applications.DeploymentResult{}, err
		}
	}
	var primary *applications.PlannedEndpoint
	for i := range plan.Endpoints {
		if plan.Endpoints[i].Primary {
			if primary != nil {
				return applications.DeploymentResult{}, applications.ErrConfigurationRequired
			}
			primary = &plan.Endpoints[i]
		}
	}
	endpointPorts := map[string]int{}
	var rollbackPorts func() error
	var newLease *providers.PortLease
	var persistedEndpoint applications.Endpoint
	if primary != nil && primary.Public {
		endpoint, err := driverutil.Endpoint(request, primary.Name)
		if err != nil {
			return applications.DeploymentResult{}, err
		}
		persistedEndpoint = endpoint
		preferredPort := primary.HostPort
		if endpoint.HostPort != nil {
			preferredPort = *endpoint.HostPort
		}
		lease, err := d.ports.Reserve(ctx, request.Application.ID, endpoint.ID, "application-"+primary.Protocol, preferred(preferredPort))
		if err != nil {
			return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageNetwork, Driver: d.Name(), Workload: primary.Workload, Operation: "reserve_port", Reason: err.Error(), Action: "choose a different published port"}
		}
		newLease = &lease
		rollback, err := d.engine.ConfigureComposePorts(ctx, request.WorkDir, projectName, primary.Workload, []providers.PublishedPort{{HostPort: lease.Port, ContainerPort: primary.ContainerPort}})
		if err != nil {
			_ = d.ports.Release(context.Background(), request.Application.ID, endpoint.ID, lease.Port)
			return applications.DeploymentResult{}, err
		}
		rollbackPorts = rollback
		endpointPorts[primary.Name] = lease.Port
	}
	labels := map[string]map[string]string{}
	for _, workload := range request.Workloads {
		values := driverutil.Labels(request.Application, request.Deployment, workload.Name)
		values["io.devbox.workload.id"] = workload.ID
		labels[workload.Name] = values
	}
	if err := d.engine.ComposeUpApplication(ctx, request.WorkDir, projectName, labels); err != nil {
		if rollbackPorts != nil {
			_ = rollbackPorts()
		}
		if newLease != nil {
			_ = d.ports.Release(context.Background(), request.Application.ID, persistedEndpoint.ID, newLease.Port)
		}
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageStart, Driver: d.Name(), Operation: "compose_up", Reason: err.Error(), Action: "previous Compose resources remain available when Compose rollback permits"}
	}
	if d.sharedNetwork != "" {
		if err := d.networks.ConnectComposeProjectNetwork(ctx, request.WorkDir, projectName, d.sharedNetwork); err != nil {
			return applications.DeploymentResult{}, err
		}
	}
	if err := d.engine.ComposeHealthy(ctx, request.WorkDir, projectName); err != nil {
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageHealthcheck, Driver: d.Name(), Operation: "compose_health", Reason: err.Error(), Action: "inspect the failing workload health/logs"}
	}
	processes, err := d.engine.ComposePS(ctx, request.WorkDir, projectName)
	if err != nil {
		return applications.DeploymentResult{}, err
	}
	result := applications.DeploymentResult{Resources: map[string]applications.ResourceState{}, EndpointPorts: endpointPorts}
	for _, process := range processes {
		observed := applications.ObservedUnknown
		switch strings.ToLower(process.State) {
		case "running":
			observed = applications.ObservedRunning
		case "exited", "dead":
			observed = applications.ObservedExited
		case "created", "restarting":
			observed = applications.ObservedStarting
		}
		health := applications.HealthUnknown
		switch strings.ToLower(process.Health) {
		case "healthy":
			health = applications.HealthHealthy
		case "unhealthy":
			health = applications.HealthUnhealthy
		}
		result.Resources[process.Service] = applications.ResourceState{ResourceID: process.Name, Image: process.Image, ObservedState: observed, HealthState: health}
	}
	if newLease != nil && persistedEndpoint.HostPort != nil && *persistedEndpoint.HostPort != newLease.Port {
		_ = d.ports.Release(context.Background(), request.Application.ID, persistedEndpoint.ID, *persistedEndpoint.HostPort)
	}
	return result, nil
}

func (d *Driver) Inspect(ctx context.Context, request applications.InspectRequest) ([]applications.ObservedWorkload, error) {
	processes, err := d.engine.ComposePS(ctx, request.WorkDir, request.Application.Slug)
	if err != nil {
		if driverutil.MissingError(err) {
			out := []applications.ObservedWorkload{}
			for _, w := range request.Workloads {
				out = append(out, applications.ObservedWorkload{Name: w.Name, ObservedState: applications.ObservedMissing, HealthState: applications.HealthUnknown})
			}
			return out, nil
		}
		return nil, err
	}
	byService := map[string]dockerapi.ComposeProcess{}
	for _, process := range processes {
		byService[process.Service] = process
	}
	out := []applications.ObservedWorkload{}
	for _, w := range request.Workloads {
		process, ok := byService[w.Name]
		if !ok {
			out = append(out, applications.ObservedWorkload{Name: w.Name, ResourceID: w.DriverResourceID, ObservedState: applications.ObservedMissing, HealthState: applications.HealthUnknown})
			continue
		}
		observed := applications.ObservedUnknown
		switch strings.ToLower(process.State) {
		case "running":
			observed = applications.ObservedRunning
		case "exited", "dead":
			observed = applications.ObservedExited
		case "created", "restarting":
			observed = applications.ObservedStarting
		}
		health := applications.HealthUnknown
		switch strings.ToLower(process.Health) {
		case "healthy":
			health = applications.HealthHealthy
		case "unhealthy":
			health = applications.HealthUnhealthy
		}
		out = append(out, applications.ObservedWorkload{Name: w.Name, ResourceID: process.Name, Image: process.Image, ObservedState: observed, HealthState: health})
	}
	return out, nil
}
func (d *Driver) Start(ctx context.Context, r applications.InspectRequest) error {
	return d.engine.ComposeStart(ctx, r.WorkDir, r.Application.Slug)
}
func (d *Driver) Stop(ctx context.Context, r applications.InspectRequest) error {
	return d.engine.ComposeStop(ctx, r.WorkDir, r.Application.Slug)
}
func (d *Driver) Restart(ctx context.Context, r applications.InspectRequest) error {
	return d.engine.ComposeRestart(ctx, r.WorkDir, r.Application.Slug, "")
}
func (d *Driver) Remove(ctx context.Context, r applications.InspectRequest) error {
	return d.engine.ComposeDown(ctx, r.WorkDir, r.Application.Slug)
}

func composeProjectName(request applications.DetectRequest) string {
	if slug := driverutil.ConfigString(request.Configuration, "slug"); slug != "" {
		return slug
	}
	return "devbox-detect"
}
func countService(items []providers.ComposePortCandidate, name string) int {
	n := 0
	for _, item := range items {
		if item.Service == name {
			n++
		}
	}
	return n
}
func defaultProtocol(value string) string {
	if value == "" {
		return "http"
	}
	return value
}
func preferred(port int) *int {
	if port <= 0 {
		return nil
	}
	return &port
}
func nonEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

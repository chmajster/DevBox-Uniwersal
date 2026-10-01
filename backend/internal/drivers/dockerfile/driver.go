package dockerfile

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/driverutil"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type engine interface {
	Available(context.Context) error
	ManagedImageExists(context.Context, string) (bool, error)
	BuildManaged(context.Context, containerspec.DeploymentSpec) error
	ReplaceManagedPorts(context.Context, containerspec.DeploymentSpec, []providers.PublishedPort) error
	Inspect(context.Context, string) (providers.ContainerInfo, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Restart(context.Context, string) error
	Remove(context.Context, string) error
}
type networkProvider interface {
	EnsureNetwork(context.Context, string) error
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
func (d *Driver) Name() string { return "dockerfile" }

func (d *Driver) Detect(_ context.Context, request applications.DetectRequest) (applications.DetectionResult, error) {
	if request.WorkDir == "" || !fileExists(request.WorkDir+"/Dockerfile") {
		return applications.DetectionResult{}, applications.ErrConfigurationRequired
	}
	spec, err := containerspec.GenerateCustomDockerfile("detect", request.WorkDir, 1)
	if err != nil {
		return applications.DetectionResult{}, err
	}
	return applications.DetectionResult{Driver: d.Name(), Confidence: "high",
		Services:  []applications.ServiceDetection{{Name: "web", SuggestedRole: "web", Primary: true, Confidence: "high", Reason: "Dockerfile defines one application image"}},
		Endpoints: []applications.EndpointDetection{{Service: "web", Protocol: "http", ContainerPort: spec.ContainerPort, Primary: true, Confidence: "medium", Reason: "Dockerfile EXPOSE or DevBox fallback"}},
		Reasons:   []string{"Dockerfile detected"}}, nil
}

func (d *Driver) Plan(_ context.Context, request applications.PlanRequest) (applications.DeploymentPlan, error) {
	spec, err := containerspec.GenerateCustomDockerfile(request.Application.ID, request.WorkDir, 1)
	if err != nil {
		return applications.DeploymentPlan{}, err
	}
	endpoint := applications.PlannedEndpoint{Name: "web", Workload: "web", Protocol: "http", ContainerPort: spec.ContainerPort, Public: true, Primary: true, HealthPath: "/"}
	role := "web"
	if manifest, err := applications.LoadManifest(request.WorkDir); err != nil {
		return applications.DeploymentPlan{}, err
	} else if manifest != nil {
		for _, workload := range manifest.Workloads {
			if workload.Role != "" {
				role = workload.Role
			}
		}
		for name, item := range manifest.Endpoints {
			endpoint = applications.PlannedEndpoint{Name: name, Workload: item.Workload, Protocol: item.Protocol, ContainerPort: item.ContainerPort, Public: item.Public, Primary: item.Primary, HealthPath: item.HealthPath}
			if endpoint.Protocol == "" {
				endpoint.Protocol = "http"
			}
			break
		}
	}
	return applications.DeploymentPlan{Version: 1, ApplicationID: request.Application.ID, Driver: d.Name(), SourceRevision: request.SourceRevision,
		Workloads: []applications.PlannedWorkload{{Name: "web", Role: role, Image: spec.Image, Primary: true}},
		Endpoints: []applications.PlannedEndpoint{endpoint}, Networks: nonEmpty(d.sharedNetwork),
		Healthchecks: []applications.HealthCheckPlan{{Workload: "web", Type: "http", Path: endpoint.HealthPath, Port: endpoint.ContainerPort}},
		Metadata:     map[string]any{"fingerprint": spec.Fingerprint}}, nil
}

func (d *Driver) Deploy(ctx context.Context, request applications.ExecutionRequest, plan applications.DeploymentPlan) (applications.DeploymentResult, error) {
	if err := d.engine.Available(ctx); err != nil {
		return applications.DeploymentResult{}, fmt.Errorf("%w: docker: %v", applications.ErrProviderUnavailable, err)
	}
	if len(plan.Workloads) != 1 || len(plan.Endpoints) != 1 {
		return applications.DeploymentResult{}, fmt.Errorf("%w: Dockerfile driver requires one workload and endpoint", applications.ErrInvalidInput)
	}
	workloadPlan, endpointPlan := plan.Workloads[0], plan.Endpoints[0]
	endpoint, err := driverutil.Endpoint(request, endpointPlan.Name)
	if err != nil {
		return applications.DeploymentResult{}, err
	}
	lease, err := d.ports.Reserve(ctx, request.Application.ID, endpoint.ID, "application-http", preferred(endpointPlan.HostPort))
	if err != nil {
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageNetwork, Driver: d.Name(), Workload: workloadPlan.Name, Operation: "reserve_port", Reason: err.Error(), Action: "change the requested port or release the collision"}
	}
	spec, err := containerspec.GenerateCustomDockerfile(request.Application.ID, request.WorkDir, lease.Port)
	if err != nil {
		release(d, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, err
	}
	spec.Labels = driverutil.MergeLabels(spec.Labels, driverutil.Labels(request.Application, request.Deployment, workloadPlan.Name))
	if d.sharedNetwork != "" {
		if d.networks == nil {
			release(d, request, endpoint, lease.Port)
			return applications.DeploymentResult{}, fmt.Errorf("%w: network provider", applications.ErrProviderUnavailable)
		}
		if err := d.networks.EnsureNetwork(ctx, d.sharedNetwork); err != nil {
			release(d, request, endpoint, lease.Port)
			return applications.DeploymentResult{}, err
		}
		spec.Networks = append(spec.Networks, d.sharedNetwork)
	}
	exists, err := d.engine.ManagedImageExists(ctx, spec.Image)
	if err != nil {
		release(d, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, err
	}
	if !exists {
		if err := d.engine.BuildManaged(ctx, spec); err != nil {
			release(d, request, endpoint, lease.Port)
			return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageBuildOrPull, Driver: d.Name(), Workload: workloadPlan.Name, Operation: "docker_build", Reason: err.Error(), Action: "fix the Dockerfile/build context"}
		}
	}
	if err := d.engine.ReplaceManagedPorts(ctx, spec, nil); err != nil {
		release(d, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageHealthcheck, Driver: d.Name(), Workload: workloadPlan.Name, Operation: "replace_container", Reason: err.Error(), Action: "previous container was restored when possible"}
	}
	if endpoint.HostPort != nil && *endpoint.HostPort != lease.Port {
		_ = d.ports.Release(context.Background(), request.Application.ID, endpoint.ID, *endpoint.HostPort)
	}
	info, err := d.engine.Inspect(ctx, spec.ContainerName)
	if err != nil {
		return applications.DeploymentResult{}, err
	}
	observed, health := driverutil.Observed(info)
	return applications.DeploymentResult{Resources: map[string]applications.ResourceState{workloadPlan.Name: {ResourceID: spec.ContainerName, Image: spec.Image, ObservedState: observed, HealthState: health}}, EndpointPorts: map[string]int{endpointPlan.Name: lease.Port}}, nil
}

func (d *Driver) Inspect(ctx context.Context, request applications.InspectRequest) ([]applications.ObservedWorkload, error) {
	out := []applications.ObservedWorkload{}
	for _, workload := range request.Workloads {
		if workload.DriverResourceID == "" {
			out = append(out, applications.ObservedWorkload{Name: workload.Name, ObservedState: applications.ObservedMissing, HealthState: applications.HealthUnknown})
			continue
		}
		info, err := d.engine.Inspect(ctx, workload.DriverResourceID)
		if err != nil {
			if driverutil.MissingError(err) {
				out = append(out, applications.ObservedWorkload{Name: workload.Name, ResourceID: workload.DriverResourceID, ObservedState: applications.ObservedMissing, HealthState: applications.HealthUnknown})
				continue
			}
			return nil, err
		}
		observed, health := driverutil.Observed(info)
		out = append(out, applications.ObservedWorkload{Name: workload.Name, ResourceID: workload.DriverResourceID, Image: info.Image, ObservedState: observed, HealthState: health})
	}
	return out, nil
}
func (d *Driver) Start(ctx context.Context, r applications.InspectRequest) error {
	return d.each(ctx, r, d.engine.Start)
}
func (d *Driver) Stop(ctx context.Context, r applications.InspectRequest) error {
	return d.each(ctx, r, d.engine.Stop)
}
func (d *Driver) Restart(ctx context.Context, r applications.InspectRequest) error {
	return d.each(ctx, r, d.engine.Restart)
}
func (d *Driver) Remove(ctx context.Context, r applications.InspectRequest) error {
	return d.each(ctx, r, func(ctx context.Context, id string) error {
		_ = d.engine.Stop(ctx, id)
		return d.engine.Remove(ctx, id)
	})
}
func (d *Driver) each(ctx context.Context, r applications.InspectRequest, action func(context.Context, string) error) error {
	for _, w := range r.Workloads {
		if w.DriverResourceID == "" {
			continue
		}
		if err := action(ctx, w.DriverResourceID); err != nil && !driverutil.MissingError(err) {
			return err
		}
	}
	return nil
}
func preferred(port int) *int {
	if port <= 0 {
		return nil
	}
	return &port
}
func release(d *Driver, r applications.ExecutionRequest, e applications.Endpoint, port int) {
	_ = d.ports.Release(context.Background(), r.Application.ID, e.ID, port)
}
func nonEmpty(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return []string{v}
}
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

package image

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/driverutil"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type engine interface {
	Available(context.Context) error
	PullImage(context.Context, string) error
	Create(context.Context, providers.ContainerSpec) (providers.ContainerInfo, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Restart(context.Context, string) error
	Remove(context.Context, string) error
	Inspect(context.Context, string) (providers.ContainerInfo, error)
	InspectImage(context.Context, string) (map[string]any, error)
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
func (d *Driver) Name() string { return "image" }

func (d *Driver) Detect(ctx context.Context, request applications.DetectRequest) (applications.DetectionResult, error) {
	image := strings.TrimSpace(request.Source.DockerImage)
	if image == "" {
		return applications.DetectionResult{}, applications.ErrConfigurationRequired
	}
	port := driverutil.ConfigInt(request.Configuration, "container_port")
	reason := "container_port configured explicitly"
	if port == 0 {
		if raw, err := d.engine.InspectImage(ctx, image); err == nil {
			port = firstExposedPort(raw)
			reason = "single HTTP-like EXPOSE port found in image metadata"
		}
	}
	result := applications.DetectionResult{Driver: d.Name(), Confidence: "high", Services: []applications.ServiceDetection{{Name: "app", SuggestedRole: "web", Primary: true, Confidence: "high", Reason: "single OCI image source"}}, Reasons: []string{"Docker/OCI image source selected"}}
	if port > 0 {
		result.Endpoints = []applications.EndpointDetection{{Service: "app", Protocol: "http", ContainerPort: port, Primary: true, Confidence: "medium", Reason: reason}}
	} else {
		result.RequiresConfiguration = true
		result.Warnings = []string{"image exposes no unambiguous application port; configure container_port"}
	}
	return result, nil
}

func (d *Driver) Plan(ctx context.Context, request applications.PlanRequest) (applications.DeploymentPlan, error) {
	image := strings.TrimSpace(request.Source.DockerImage)
	if image == "" {
		return applications.DeploymentPlan{}, fmt.Errorf("%w: docker image is required", applications.ErrInvalidInput)
	}
	port := driverutil.ConfigInt(request.Configuration, "container_port")
	if port == 0 {
		if len(request.Detection.Endpoints) == 1 {
			port = request.Detection.Endpoints[0].ContainerPort
		}
	}
	if port == 0 {
		if raw, err := d.engine.InspectImage(ctx, image); err == nil {
			port = firstExposedPort(raw)
		}
	}
	if port == 0 {
		if err := d.engine.PullImage(ctx, image); err != nil {
			return applications.DeploymentPlan{}, err
		}
		if raw, err := d.engine.InspectImage(ctx, image); err == nil {
			port = firstExposedPort(raw)
		}
	}
	if port == 0 {
		return applications.DeploymentPlan{}, applications.ErrConfigurationRequired
	}
	protocol := driverutil.ConfigString(request.Configuration, "protocol")
	if protocol == "" {
		protocol = "http"
	}
	endpoint := applications.PlannedEndpoint{Name: "main", Workload: "app", Protocol: protocol, ContainerPort: port, HostPort: driverutil.ConfigInt(request.Configuration, "host_port"), Public: true, Primary: true, HealthPath: driverutil.ConfigString(request.Configuration, "health_path")}
	return applications.DeploymentPlan{Version: 1, ApplicationID: request.Application.ID, Driver: d.Name(), SourceRevision: request.SourceRevision,
		Workloads: []applications.PlannedWorkload{{Name: "app", Role: "web", Image: image, Primary: true, Command: driverutil.ConfigStringSlice(request.Configuration, "command"), Environment: driverutil.ConfigStringMap(request.Configuration, "environment")}},
		Endpoints: []applications.PlannedEndpoint{endpoint}, Networks: nonEmpty(d.sharedNetwork), Healthchecks: healthPlan(endpoint),
		Metadata: map[string]any{"restart_policy": restartPolicy(request.Configuration)}}, nil
}

func (d *Driver) Deploy(ctx context.Context, request applications.ExecutionRequest, plan applications.DeploymentPlan) (applications.DeploymentResult, error) {
	if err := d.engine.Available(ctx); err != nil {
		return applications.DeploymentResult{}, fmt.Errorf("%w: docker: %v", applications.ErrProviderUnavailable, err)
	}
	if len(plan.Workloads) != 1 || len(plan.Endpoints) != 1 {
		return applications.DeploymentResult{}, fmt.Errorf("%w: image driver requires one workload and endpoint", applications.ErrInvalidInput)
	}
	wp, ep := plan.Workloads[0], plan.Endpoints[0]
	endpoint, err := driverutil.Endpoint(request, ep.Name)
	if err != nil {
		return applications.DeploymentResult{}, err
	}
	lease, err := d.ports.Reserve(ctx, request.Application.ID, endpoint.ID, "application-"+ep.Protocol, preferred(ep.HostPort))
	if err != nil {
		return applications.DeploymentResult{}, err
	}
	if d.sharedNetwork != "" {
		if d.networks == nil {
			release(d, request, endpoint, lease.Port)
			return applications.DeploymentResult{}, fmt.Errorf("%w: network provider", applications.ErrProviderUnavailable)
		}
		if err := d.networks.EnsureNetwork(ctx, d.sharedNetwork); err != nil {
			release(d, request, endpoint, lease.Port)
			return applications.DeploymentResult{}, err
		}
	}
	if err := d.engine.PullImage(ctx, wp.Image); err != nil {
		release(d, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageBuildOrPull, Driver: d.Name(), Workload: wp.Name, Operation: "pull_image", Reason: err.Error(), Action: "verify image name, registry credentials and network access"}
	}
	name := containerName(request.Application.ID, request.Deployment.ID)
	spec := providers.ContainerSpec{SensitiveEnvironment: request.SensitiveEnvironment, Name: name, Image: wp.Image, Command: wp.Command, Environment: wp.Environment, Labels: driverutil.Labels(request.Application, request.Deployment, wp.Name), Ports: map[int]int{lease.Port: ep.ContainerPort}, Networks: nonEmpty(d.sharedNetwork), RestartPolicy: restartPolicy(plan.Metadata)}
	stopped := []string{}
	restore := func() {
		for _, id := range stopped {
			_ = d.engine.Start(context.Background(), id)
		}
	}
	if endpoint.HostPort != nil && *endpoint.HostPort == lease.Port {
		for _, old := range request.Workloads {
			if old.DriverResourceID != "" && old.ObservedState == applications.ObservedRunning {
				if err := d.engine.Stop(ctx, old.DriverResourceID); err != nil {
					restore()
					release(d, request, endpoint, lease.Port)
					return applications.DeploymentResult{}, err
				}
				stopped = append(stopped, old.DriverResourceID)
			}
		}
	}
	info, err := d.engine.Create(ctx, spec)
	if err != nil {
		restore()
		release(d, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageCreate, Driver: d.Name(), Workload: wp.Name, Operation: "create_container", Reason: err.Error(), Action: "inspect Docker state and port/network collisions"}
	}
	resource := info.ID
	if resource == "" {
		resource = name
	}
	rollback := func() {
		_ = d.engine.Stop(context.Background(), resource)
		_ = d.engine.Remove(context.Background(), resource)
		restore()
		release(d, request, endpoint, lease.Port)
	}
	if err := d.engine.Start(ctx, resource); err != nil {
		rollback()
		return applications.DeploymentResult{}, err
	}
	if err := waitEndpoint(ctx, ep.Protocol, lease.Port, ep.HealthPath); err != nil {
		rollback()
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageHealthcheck, Driver: d.Name(), Workload: wp.Name, Operation: "healthcheck", Reason: err.Error(), Action: "previous resource was kept; fix listener/health configuration"}
	}
	info, err = d.engine.Inspect(ctx, resource)
	if err != nil {
		rollback()
		return applications.DeploymentResult{}, err
	}
	observed, health := driverutil.Observed(info)
	if ep.Protocol == "http" || ep.Protocol == "https" {
		health = applications.HealthHealthy
	}
	for _, old := range request.Workloads {
		if old.DriverResourceID != "" && old.DriverResourceID != resource {
			_ = d.engine.Stop(ctx, old.DriverResourceID)
			_ = d.engine.Remove(ctx, old.DriverResourceID)
		}
	}
	if endpoint.HostPort != nil && *endpoint.HostPort != lease.Port {
		_ = d.ports.Release(context.Background(), request.Application.ID, endpoint.ID, *endpoint.HostPort)
	}
	return applications.DeploymentResult{Resources: map[string]applications.ResourceState{wp.Name: {ResourceID: resource, Image: wp.Image, ObservedState: observed, HealthState: health}}, EndpointPorts: map[string]int{ep.Name: lease.Port}}, nil
}

func (d *Driver) Inspect(ctx context.Context, request applications.InspectRequest) ([]applications.ObservedWorkload, error) {
	out := []applications.ObservedWorkload{}
	for _, w := range request.Workloads {
		if w.DriverResourceID == "" {
			out = append(out, applications.ObservedWorkload{Name: w.Name, ObservedState: applications.ObservedMissing, HealthState: applications.HealthUnknown})
			continue
		}
		info, err := d.engine.Inspect(ctx, w.DriverResourceID)
		if err != nil {
			if driverutil.MissingError(err) {
				out = append(out, applications.ObservedWorkload{Name: w.Name, ResourceID: w.DriverResourceID, ObservedState: applications.ObservedMissing, HealthState: applications.HealthUnknown})
				continue
			}
			return nil, err
		}
		observed, health := driverutil.Observed(info)
		out = append(out, applications.ObservedWorkload{Name: w.Name, ResourceID: w.DriverResourceID, Image: info.Image, ObservedState: observed, HealthState: health})
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
func (d *Driver) each(ctx context.Context, r applications.InspectRequest, fn func(context.Context, string) error) error {
	for _, w := range r.Workloads {
		if w.DriverResourceID == "" {
			continue
		}
		if err := fn(ctx, w.DriverResourceID); err != nil && !driverutil.MissingError(err) {
			return err
		}
	}
	return nil
}

func firstExposedPort(raw map[string]any) int {
	config, ok := raw["Config"].(map[string]any)
	if !ok {
		return 0
	}
	ports, ok := config["ExposedPorts"].(map[string]any)
	if !ok {
		return 0
	}
	best := 0
	for key := range ports {
		if strings.HasSuffix(key, "/udp") {
			continue
		}
		part := strings.SplitN(key, "/", 2)[0]
		port, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		if port == 80 || port == 443 || port == 3000 || port == 8000 || port == 8080 || port == 8443 {
			if best != 0 {
				return 0
			}
			best = port
		}
	}
	return best
}
func containerName(appID, deploymentID string) string {
	clean := strings.NewReplacer("-", "", "_", "").Replace(appID)
	if len(clean) > 12 {
		clean = clean[:12]
	}
	dep := strings.NewReplacer("-", "", "_", "").Replace(deploymentID)
	if len(dep) > 8 {
		dep = dep[:8]
	}
	return "devbox-app-" + clean + "-" + dep
}
func waitEndpoint(ctx context.Context, protocol string, port int, path string) error {
	deadline := time.Now().Add(45 * time.Second)
	if path == "" {
		path = "/"
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		switch protocol {
		case "http", "https":
			scheme := protocol
			client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			req, e := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, port, path), nil)
			if e != nil {
				return e
			}
			resp, e := client.Do(req)
			err = e
			if e == nil {
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 500 {
					return nil
				}
				err = fmt.Errorf("HTTP status %d", resp.StatusCode)
			}
		default:
			var conn net.Conn
			conn, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("endpoint %s://127.0.0.1:%d%s did not become ready: %v", protocol, port, path, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
func healthPlan(endpoint applications.PlannedEndpoint) []applications.HealthCheckPlan {
	kind := "tcp"
	if endpoint.Protocol == "http" || endpoint.Protocol == "https" {
		kind = "http"
	}
	return []applications.HealthCheckPlan{{Workload: endpoint.Workload, Type: kind, Path: endpoint.HealthPath, Port: endpoint.ContainerPort}}
}
func restartPolicy(config map[string]any) string {
	value := driverutil.ConfigString(config, "restart_policy")
	if value == "" {
		value = "unless-stopped"
	}
	return value
}
func preferred(port int) *int {
	if port <= 0 {
		return nil
	}
	return &port
}
func release(d *Driver, r applications.ExecutionRequest, e applications.Endpoint, port int) {
	driverutil.RollbackPort(d.ports, r, e, port)
}
func nonEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

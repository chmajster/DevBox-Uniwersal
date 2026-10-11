package managed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/driverutil"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
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

var phpComposerVersion = regexp.MustCompile(`(^|[^A-Za-z0-9])(\^|~|>=?|<=?|=)?[[:space:]]*([0-9]+\.[0-9]+(\.[0-9]+)?)`)

type Driver struct {
	engine        engine
	runtimes      runtimes.Registry
	ports         applications.PortAllocator
	networks      networkProvider
	sharedNetwork string
}

func New(engine engine, registry runtimes.Registry, ports applications.PortAllocator, networks networkProvider, sharedNetwork string) *Driver {
	return &Driver{engine: engine, runtimes: registry, ports: ports, networks: networks, sharedNetwork: sharedNetwork}
}
func (d *Driver) Name() string { return "managed" }

func (d *Driver) Detect(ctx context.Context, request applications.DetectRequest) (applications.DetectionResult, error) {
	if mode := customMode(request.SourceType, request.Configuration); mode != "" {
		return d.detectCustom(request, mode)
	}
	if request.WorkDir == "" {
		return applications.DetectionResult{}, applications.ErrConfigurationRequired
	}
	port := driverutil.ConfigInt(request.Configuration, "container_port")
	if port == 0 {
		port = 8080
	}
	reload, _ := request.Configuration["development_reload"].(bool)
	configured := containerspec.NormalizeRuntime(driverutil.ConfigString(request.Configuration, "runtime"))
	candidates := []applications.RuntimeCandidate{}
	for _, name := range d.runtimes.List() {
		runtime, ok := d.runtimes.Get(name)
		if !ok {
			continue
		}
		detected, err := runtime.Detect(ctx, runtimes.ProjectContext{WorkDir: request.WorkDir, Config: request.Configuration})
		if err != nil {
			if configured == name {
				return applications.DetectionResult{}, fmt.Errorf("%w: %v", applications.ErrInvalidInput, err)
			}
			continue
		}
		if !detected.Detected {
			continue
		}
		framework, _ := detected.Metadata["framework"].(string)
		version := normalizeDetectedVersion(name, detected.Version)
		if version == "" {
			version = containerspec.DefaultVersion(name)
		}
		candidates = append(candidates, applications.RuntimeCandidate{Runtime: name, Version: version, Framework: framework, StartOptions: startOptions(request.WorkDir, name, "", port, reload)})
	}
	result := applications.DetectionResult{Driver: d.Name(), RuntimeCandidates: candidates, Confidence: "high"}
	if configured != "" {
		version := normalizeDetectedVersion(configured, driverutil.ConfigString(request.Configuration, "runtime_version"))
		if err := containerspec.Validate(configured, version, driverutil.Modules(request.Configuration)); err != nil {
			return result, fmt.Errorf("%w: %v", applications.ErrInvalidInput, err)
		}
		result.Runtime, result.Version = configured, version
		result.Reasons = []string{"runtime configured explicitly"}
		for _, candidate := range candidates {
			if candidate.Runtime == configured {
				result.Framework = candidate.Framework
			}
		}
	} else if isWordPress(request.WorkDir) {
		result.Runtime, result.Framework = "php", "WordPress"
	} else if len(candidates) == 1 {
		result.Runtime, result.Version, result.Framework = candidates[0].Runtime, candidates[0].Version, candidates[0].Framework
		result.Reasons = []string{"runtime manifest/files detected"}
	} else {
		result.RequiresConfiguration = true
		result.Confidence = "none"
		result.Warnings = []string{"select a runtime explicitly; source contains multiple technologies or no supported runtime"}
		return result, applications.ErrConfigurationRequired
	}
	if isWordPress(request.WorkDir) && result.Runtime == "php" {
		result.Profile, result.Framework = "wordpress", "WordPress"
		if driverutil.ConfigInt(request.Configuration, "container_port") == 0 {
			port = 80
		}
	}
	if result.Version == "" {
		result.Version = containerspec.DefaultVersion(result.Runtime)
	}
	result.StartOptions = startOptions(request.WorkDir, result.Runtime, result.Profile, port, reload)
	if len(result.StartOptions) == 1 && result.StartOptions[0].Framework != "" {
		result.Framework = result.StartOptions[0].Framework
	}
	if command := driverutil.ConfigString(request.Configuration, "start_command"); command != "" {
		result.StartCommand = command
	} else if len(result.StartOptions) == 1 {
		result.StartCommand = result.StartOptions[0].Command
	} else if len(result.StartOptions) > 1 && len(driverutil.ConfigStringSlice(request.Configuration, "command")) == 0 {
		result.RequiresConfiguration = true
		result.Warnings = append(result.Warnings, "multiple entrypoints detected; select a start command before deployment")
	}
	result.Services = []applications.ServiceDetection{{Name: "web", SuggestedRole: "web", Primary: true, Confidence: "high", Reason: "single selected managed workload"}}
	result.Endpoints = []applications.EndpointDetection{{Service: "web", Protocol: "http", ContainerPort: port, Primary: true, Confidence: "medium"}}
	return result, nil
}

func startOptions(root, runtime, profile string, port int, reload bool) []applications.StartOption {
	out := []applications.StartOption{}
	for _, item := range containerspec.StartOptions(root, runtime, profile, port, reload) {
		out = append(out, applications.StartOption{Name: item.Name, Framework: item.Framework, Command: item.Command})
	}
	return out
}

func normalizeDetectedVersion(runtimeName, version string) string {
	version = strings.TrimSpace(version)
	if runtimeName == "php" && version == "*" {
		return ""
	}
	if version == "" || containerspec.Validate(runtimeName, version, nil) == nil || runtimeName != "php" {
		return version
	}
	// Composer PHP requirements describe a compatible range, not an image tag.
	// Use the first explicit PHP version in that range as the concrete base tag.
	if match := phpComposerVersion.FindStringSubmatch(version); len(match) == 5 {
		return match[3]
	}
	return version
}

func (d *Driver) Plan(ctx context.Context, request applications.PlanRequest) (applications.DeploymentPlan, error) {
	if request.Detection.RequiresConfiguration {
		return applications.DeploymentPlan{}, applications.ErrConfigurationRequired
	}
	runtimeName := strings.TrimSpace(request.Detection.Runtime)
	if runtimeName == "" {
		detection, err := d.Detect(ctx, applications.DetectRequest{SourceType: request.Application.SourceType, Source: request.Source, WorkDir: request.WorkDir, Configuration: request.Configuration})
		if err != nil {
			return applications.DeploymentPlan{}, err
		}
		if detection.RequiresConfiguration {
			return applications.DeploymentPlan{}, applications.ErrConfigurationRequired
		}
		runtimeName = detection.Runtime
		request.Detection = detection
	}
	version := strings.TrimSpace(request.Detection.Version)
	if configured := driverutil.ConfigString(request.Configuration, "runtime_version"); configured != "" {
		version = configured
	}
	spec, err := d.specification(request.Application.ID, request.WorkDir, request.SourceRevision, request.Source, runtimeName, version, request.Detection.Profile, request.Configuration, 1)
	if err != nil {
		return applications.DeploymentPlan{}, err
	}
	role, primary := "web", true
	if manifest, err := applications.LoadManifest(request.WorkDir); err != nil {
		return applications.DeploymentPlan{}, err
	} else if manifest != nil {
		if len(manifest.Workloads) > 1 || len(manifest.Endpoints) > 1 {
			return applications.DeploymentPlan{}, fmt.Errorf("%w: use Compose for multiple services/endpoints", applications.ErrInvalidInput)
		}

		for _, item := range manifest.Workloads {
			if item.Role != "" {
				role = item.Role
			}
			if item.Primary {
				primary = true
			}
		}
	}
	endpoint := applications.PlannedEndpoint{Name: "web", Workload: "web", Protocol: "http", ContainerPort: spec.ContainerPort, Public: true, Primary: true, HealthPath: "/"}
	if manifest, err := applications.LoadManifest(request.WorkDir); err == nil && manifest != nil && len(manifest.Endpoints) > 0 {
		for name, item := range manifest.Endpoints {
			endpoint = applications.PlannedEndpoint{Name: name, Workload: item.Workload, Protocol: item.Protocol, ContainerPort: item.ContainerPort, Public: item.Public, Primary: item.Primary, HealthPath: item.HealthPath}
			if endpoint.Protocol == "" {
				endpoint.Protocol = "http"
			}
			break
		}
	}
	command, commandErr := containerspec.ParseStartCommand(driverutil.ConfigString(request.Configuration, "start_command"))
	if commandErr != nil {
		return applications.DeploymentPlan{}, fmt.Errorf("%w: %v", applications.ErrInvalidInput, commandErr)
	}
	if len(command) == 0 {
		command = driverutil.ConfigStringSlice(request.Configuration, "command")
	}
	if len(command) == 0 && (runtimeName == "go" || runtimeName == "python") {
		command, _ = containerspec.ParseStartCommand(request.Detection.StartCommand)
	}
	if reload, configured := request.Configuration["development_reload"].(bool); configured {
		command = containerspec.ApplyDevelopmentReload(command, runtimeName, reload)
	}
	endpoint = driverutil.ConfigureEndpoint(endpoint, request.Configuration)
	endpoint.Workload = "web"
	if endpoint.Protocol != "http" {
		return applications.DeploymentPlan{}, fmt.Errorf("%w: managed runtimes support HTTP only", applications.ErrInvalidInput)
	}
	volumes := make([]applications.Volume, 0, len(spec.BindMounts)+len(spec.AnonymousVolumes))
	for source, target := range spec.BindMounts {
		volumes = append(volumes, applications.Volume{ID: request.Application.ID + ":bind:" + target, ApplicationID: request.Application.ID, Type: "bind", Source: source, Target: target, Persistent: true})
	}
	for _, target := range spec.AnonymousVolumes {
		volumes = append(volumes, applications.Volume{ID: request.Application.ID + ":volume:" + target, ApplicationID: request.Application.ID, Type: "volume", Target: target})
	}
	sourcePath, _ := filepath.Abs(request.WorkDir)
	if request.Application.SourceType == applications.SourceImage {
		sourcePath = ""
	}
	profile := request.Detection.Profile
	if profile == "" {
		profile = "generic_" + runtimeName
	}
	containerName := spec.ContainerName
	networks := []string{applications.DockerProjectName(request.Application.ID)}
	if d.sharedNetwork != "" {
		networks = append(networks, d.sharedNetwork)
	}
	return applications.DeploymentPlan{
		Version: 1, ApplicationID: request.Application.ID, Driver: d.Name(), SourceRevision: request.SourceRevision,
		Runtime:   &applications.Runtime{ApplicationID: request.Application.ID, Name: runtimeName, Version: spec.Version, Metadata: map[string]any{"adapter": "managed", "profile": profile, "generated": spec.Runtime != "custom", "fingerprint": spec.Fingerprint, "container_name": containerName, "source_path": sourcePath, "container_port": spec.ContainerPort, "network": networks[0], "networks": networks, "volumes": volumes}},
		Workloads: []applications.PlannedWorkload{{Name: "web", Role: role, Primary: primary, Runtime: runtimeName, Image: spec.Image, Command: command, Environment: driverutil.ConfigStringMap(request.Configuration, "environment")}},
		Endpoints: []applications.PlannedEndpoint{endpoint}, Volumes: volumes,
		Networks: networks, Healthchecks: []applications.HealthCheckPlan{{Workload: "web", Type: "http", Path: endpoint.HealthPath, Port: endpoint.ContainerPort}},
		Metadata: map[string]any{"fingerprint": spec.Fingerprint, "modules": driverutil.ConfigStringSlice(request.Configuration, "modules"), "configuration": request.Configuration},
	}, nil
}

func (d *Driver) Deploy(ctx context.Context, request applications.ExecutionRequest, plan applications.DeploymentPlan) (applications.DeploymentResult, error) {
	if err := d.engine.Available(ctx); err != nil {
		return applications.DeploymentResult{}, fmt.Errorf("%w: docker: %v", applications.ErrProviderUnavailable, err)
	}
	if len(plan.Workloads) != 1 || len(plan.Endpoints) != 1 {
		return applications.DeploymentResult{}, fmt.Errorf("%w: managed driver requires one workload and endpoint", applications.ErrInvalidInput)
	}
	workloadPlan, endpointPlan := plan.Workloads[0], plan.Endpoints[0]
	endpoint, err := driverutil.Endpoint(request, endpointPlan.Name)
	if err != nil {
		return applications.DeploymentResult{}, err
	}
	lease, err := d.ports.Reserve(ctx, request.Application.ID, endpoint.ID, "application-http", preferred(endpointPlan.HostPort))
	if err != nil {
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageNetwork, Driver: d.Name(), Workload: workloadPlan.Name, Operation: "reserve_port", Reason: err.Error(), Action: "change the requested host port or release the collision"}
	}
	profile, _ := plan.Runtime.Metadata["profile"].(string)
	config, _ := plan.Metadata["configuration"].(map[string]any)
	if config == nil {
		config = plan.Metadata
	}
	spec, err := d.specification(request.Application.ID, request.WorkDir, plan.SourceRevision, request.Source, plan.Runtime.Name, plan.Runtime.Version, profile, config, lease.Port)
	if err != nil {
		driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, err
	}
	spec, err = driverutil.ApplyListener(spec, endpointPlan, spec.Runtime != "custom")
	if err != nil {
		driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, err
	}
	for key, value := range workloadPlan.Environment {
		if spec.Environment == nil {
			spec.Environment = map[string]string{}
		}
		spec.Environment[key] = value
	}
	spec.Command = workloadPlan.Command
	spec.SensitiveEnvironment = request.SensitiveEnvironment
	spec.HealthPath = endpointPlan.HealthPath
	spec.Labels = driverutil.MergeLabels(spec.Labels, driverutil.Labels(request.Application, request.Deployment, workloadPlan.Name))
	privateNetwork := applications.DockerProjectName(request.Application.ID)
	if d.networks == nil {
		driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, applications.ErrProviderUnavailable
	}
	if provider, ok := d.networks.(interface {
		EnsureApplicationNetwork(context.Context, string, map[string]string) error
	}); ok {
		err = provider.EnsureApplicationNetwork(ctx, privateNetwork, spec.Labels)
	} else {
		err = d.networks.EnsureNetwork(ctx, privateNetwork)
	}
	if err != nil {
		driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, err
	}
	spec.Networks = []string{privateNetwork}
	if d.sharedNetwork != "" {
		if err := d.networks.EnsureNetwork(ctx, d.sharedNetwork); err != nil {
			driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
			return applications.DeploymentResult{}, err
		}
		spec.Networks = append(spec.Networks, d.sharedNetwork)
	}
	if err := d.ensureBaseImages(ctx, spec); err != nil {
		driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageDependencies, Driver: d.Name(), Operation: "runtime_image", Reason: err.Error(), Action: "choose an existing image/runtime tag or restore registry access"}
	}
	exists, err := d.engine.ManagedImageExists(ctx, spec.Image)
	if err != nil {
		driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, err
	}
	if spec.DockerfilePath != "" || (spec.Runtime != "custom" && (!exists || request.ForceBuild)) {
		if request.Progress != nil {
			request.Progress(applications.StageBuildOrPull, 35)
		}
		if err := d.engine.BuildManaged(ctx, spec); err != nil {
			driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
			return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageBuildOrPull, Driver: d.Name(), Workload: workloadPlan.Name, Operation: "build_image", Reason: err.Error(), Action: "inspect build logs and runtime dependencies", BuildLog: applications.BuildLogFromError(err), Cause: err}
		}
	}
	if request.Progress != nil {
		request.Progress(applications.StageCreate, 65)
	}
	if verifier, ok := d.engine.(interface {
		VerifyManagedSource(context.Context, containerspec.DeploymentSpec) error
	}); ok {
		if err := verifier.VerifyManagedSource(ctx, spec); err != nil {
			driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
			return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageStorage, Driver: d.Name(), Operation: "verify_source_mount", Reason: err.Error(), Action: "check Docker daemon path mapping and source permissions"}
		}
	}
	if err := d.engine.ReplaceManagedPorts(ctx, spec, nil); err != nil {
		driverutil.RollbackPort(d.ports, request, endpoint, lease.Port)
		return applications.DeploymentResult{}, &applications.OperationError{Stage: applications.StageHealthcheck, Driver: d.Name(), Workload: workloadPlan.Name, Operation: "replace_container", Reason: err.Error(), Action: "previous container was restored when possible"}
	}
	if provider, ok := d.engine.(interface {
		SaveManagedRuntime(containerspec.DeploymentSpec) error
	}); ok {
		if err := provider.SaveManagedRuntime(spec); err != nil {
			return applications.DeploymentResult{}, fmt.Errorf("application started, but runtime export could not be saved: %w", err)
		}
	}
	if endpoint.HostPort != nil && *endpoint.HostPort != lease.Port {
		_ = d.ports.Release(context.Background(), request.Application.ID, endpoint.ID, *endpoint.HostPort)
	}
	info, err := d.engine.Inspect(ctx, spec.ContainerName)
	if err != nil {
		return applications.DeploymentResult{}, err
	}
	observed, health := driverutil.Observed(info)
	return applications.DeploymentResult{
		Resources:     map[string]applications.ResourceState{workloadPlan.Name: {ResourceID: spec.ContainerName, Image: spec.Image, ObservedState: observed, HealthState: health}},
		EndpointPorts: map[string]int{endpointPlan.Name: lease.Port},
	}, nil
}

func isWordPress(root string) bool {
	markers := []string{"wp-admin", "wp-includes", "wp-content"}
	count := 0
	for _, name := range markers {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.IsDir() {
			count++
		}
	}
	for _, name := range []string{"wp-login.php", "wp-settings.php"} {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
			count++
		}
	}
	return count >= 2
}

func (d *Driver) Inspect(ctx context.Context, request applications.InspectRequest) ([]applications.ObservedWorkload, error) {
	out := make([]applications.ObservedWorkload, 0, len(request.Workloads))
	for _, workload := range request.Workloads {
		id := workload.DriverResourceID
		if id == "" {
			suffix := request.Application.ID
			if len(suffix) > 24 {
				suffix = strings.TrimRight(suffix[:24], "-")
			}
			id = "devbox-app-" + suffix
		}
		info, err := d.engine.Inspect(ctx, id)
		if err != nil {
			if driverutil.MissingError(err) {
				out = append(out, applications.ObservedWorkload{Name: workload.Name, ResourceID: id, ObservedState: applications.ObservedMissing, HealthState: applications.HealthUnknown})
				continue
			}
			return nil, err
		}
		observed, health := driverutil.Observed(info)
		if observed == applications.ObservedRunning && info.PortBindings != nil {
			for _, endpoint := range request.Endpoints {
				if endpoint.WorkloadID != workload.ID || !endpoint.Public || endpoint.HostPort == nil {
					continue
				}
				found := false
				for _, binding := range info.PortBindings {
					if binding.ContainerPort == endpoint.ContainerPort && binding.HostPort == *endpoint.HostPort {
						found = true
					}
				}
				if !found {
					health = applications.HealthUnhealthy
				}
			}
		}
		out = append(out, applications.ObservedWorkload{Name: workload.Name, ResourceID: id, Image: info.Image, ObservedState: observed, HealthState: health, PortBindings: info.PortBindings})
	}
	return out, nil
}

func (d *Driver) Start(ctx context.Context, request applications.InspectRequest) error {
	return d.each(ctx, request, d.engine.Start)
}
func (d *Driver) Stop(ctx context.Context, request applications.InspectRequest) error {
	return d.each(ctx, request, d.engine.Stop)
}
func (d *Driver) Restart(ctx context.Context, request applications.InspectRequest) error {
	return d.each(ctx, request, d.engine.Restart)
}
func (d *Driver) Remove(ctx context.Context, request applications.InspectRequest) error {
	err := d.each(ctx, request, func(ctx context.Context, id string) error {
		_ = d.engine.Stop(ctx, id)
		return d.engine.Remove(ctx, id)
	})
	if err != nil {
		return err
	}
	if provider, ok := d.networks.(interface {
		RemoveApplicationNetwork(context.Context, string) error
	}); ok {
		return provider.RemoveApplicationNetwork(ctx, applications.DockerProjectName(request.Application.ID))
	}
	return nil
}
func (d *Driver) each(ctx context.Context, request applications.InspectRequest, action func(context.Context, string) error) error {
	for _, workload := range request.Workloads {
		if workload.DriverResourceID == "" {
			continue
		}
		if verifier, ok := d.engine.(interface {
			VerifyApplicationContainer(context.Context, string, string) error
		}); ok {
			if err := verifier.VerifyApplicationContainer(ctx, workload.DriverResourceID, request.Application.ID); err != nil {
				if driverutil.MissingError(err) {
					continue
				}
				return err
			}
		}
		if err := action(ctx, workload.DriverResourceID); err != nil && !driverutil.MissingError(err) {
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
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func nonEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

func (d *Driver) Build(ctx context.Context, request applications.ExecutionRequest, plan applications.DeploymentPlan) error {
	profile, _ := plan.Runtime.Metadata["profile"].(string)
	config, _ := plan.Metadata["configuration"].(map[string]any)
	if config == nil {
		config = plan.Metadata
	}
	spec, err := d.specification(request.Application.ID, request.WorkDir, plan.SourceRevision, request.Source, plan.Runtime.Name, plan.Runtime.Version, profile, config, 1)
	if err != nil {
		return err
	}
	spec, err = driverutil.ApplyListener(spec, plan.Endpoints[0], spec.Runtime != "custom")
	if err != nil {
		return err
	}
	spec.Labels = driverutil.MergeLabels(spec.Labels, driverutil.Labels(request.Application, request.Deployment, "web"))
	if err := d.ensureBaseImages(ctx, spec); err != nil {
		return err
	}
	if spec.Runtime == "custom" && spec.DockerfilePath == "" {
		return nil
	}
	return d.engine.BuildManaged(ctx, spec)
}

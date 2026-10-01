package applications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type JobLogger interface {
	Log(context.Context, string, string, string, map[string]any) error
}

type detectJobHandler struct {
	service *Service
	logger  JobLogger
}

func NewDetectJobHandler(service *Service, logger JobLogger) jobs.Handler {
	return &detectJobHandler{service: service, logger: logger}
}
func (h *detectJobHandler) Type() string { return JobDetect }
func (h *detectJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	input, err := decodeCreateInput(job.Payload)
	if err != nil {
		return nil, err
	}
	if input.SourceType != SourceGit {
		return nil, fmt.Errorf("%w: async detection is only required for Git sources", ErrInvalidInput)
	}
	if h.service.git == nil {
		return nil, fmt.Errorf("%w: git", ErrProviderUnavailable)
	}
	tempRoot := filepath.Join(h.service.projectsRoot, ".analysis")
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, err
	}
	workDir := filepath.Join(tempRoot, job.ID)
	defer os.RemoveAll(workDir)
	source := Source{RepositoryURL: input.Source.RepositoryURL, Reference: input.Source.Reference, CredentialID: input.Source.CredentialID}
	credentialRef, err := h.service.gitCredentialRef(ctx, source.CredentialID)
	if err != nil {
		return nil, err
	}
	if err := h.service.git.Clone(ctx, providers.GitSource{RepositoryURL: source.RepositoryURL, Reference: source.Reference, Destination: workDir, CredentialRef: credentialRef}); err != nil {
		return nil, err
	}
	result, detectErr := h.service.selector.Detect(ctx, DetectRequest{SourceType: SourceGit, Source: source, WorkDir: workDir, Configuration: input.Configuration}, input.Driver)
	if detectErr != nil && !errors.Is(detectErr, ErrConfigurationRequired) {
		return nil, detectErr
	}
	raw, _ := json.Marshal(result)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return map[string]any{"detection": out}, nil
}

type deployJobHandler struct {
	service *Service
	logger  JobLogger
}

func NewDeployJobHandler(service *Service, logger JobLogger) jobs.Handler {
	return &deployJobHandler{service: service, logger: logger}
}
func (h *deployJobHandler) Type() string { return JobDeploy }
func (h *deployJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	appID, _ := job.Payload["application_id"].(string)
	deploymentID, _ := job.Payload["deployment_id"].(string)
	if appID == "" || deploymentID == "" {
		return nil, fmt.Errorf("%w: application_id and deployment_id", ErrInvalidInput)
	}
	app, err := h.service.repo.Get(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageSource, err)
	}
	source, err := h.service.repo.Source(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageSource, err)
	}
	deployment, err := h.service.repo.Deployment(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	setStage := func(stage string) {
		_ = h.service.repo.SetDeploymentStage(ctx, deploymentID, "running", stage)
		_ = h.service.repo.Event(ctx, appID, deploymentID, "", "deployment.stage", stage, "", map[string]any{"stage": stage})
		if h.logger != nil {
			_ = h.logger.Log(ctx, job.ID, "info", "application.deployment.stage", map[string]any{"stage": stage, "application_id": appID, "deployment_id": deploymentID})
		}
	}
	if err := h.service.repo.BindDeploymentJob(ctx, deploymentID, job.ID); err != nil {
		return nil, err
	}
	setStage(StageSource)
	workDir, revision, err := h.service.prepareSource(ctx, app, source)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageSource, err)
	}
	source.CurrentRevision = revision
	_ = h.service.repo.UpdateSource(ctx, source)
	setStage(StageDetect)
	config := decodeConfiguration(app.SourceConfig)
	manifest, manifestErr := LoadManifest(workDir)
	if workDir == "" {
		manifest = nil
		manifestErr = nil
	}
	if manifestErr != nil {
		return nil, h.fail(ctx, deploymentID, StageDetect, manifestErr)
	}
	if manifest != nil && len(manifest.Environment) > 0 {
		env := map[string]any{}
		for k, v := range manifest.Environment {
			env[k] = v
		}
		if explicit, ok := config["environment"].(map[string]any); ok {
			for k, v := range explicit {
				env[k] = v
			}
		}
		config["environment"] = env
		if err := validateConfiguration(config); err != nil {
			return nil, h.fail(ctx, deploymentID, StageDetect, err)
		}
	}
	detection, detectErr := h.service.selector.Detect(ctx, DetectRequest{SourceType: app.SourceType, Source: source, WorkDir: workDir, Configuration: config}, app.Driver)
	if detectErr != nil && !errors.Is(detectErr, ErrConfigurationRequired) {
		return nil, h.fail(ctx, deploymentID, StageDetect, detectErr)
	}
	if detection.Driver == "" {
		if app.Driver != "" {
			detection.Driver = app.Driver
		} else {
			return h.waiting(ctx, appID, deploymentID, detection)
		}
	}
	driver, ok := h.service.drivers.Get(detection.Driver)
	if !ok {
		return nil, h.fail(ctx, deploymentID, StageDetect, ErrProviderUnavailable)
	}
	setStage(StagePlan)
	plan, err := driver.Plan(ctx, PlanRequest{Application: app, Source: source, WorkDir: workDir, Detection: detection, Configuration: config, SourceRevision: revision})
	if err != nil {
		if errors.Is(err, ErrConfigurationRequired) {
			return h.waiting(ctx, appID, deploymentID, detection)
		}
		return nil, h.fail(ctx, deploymentID, StagePlan, err)
	}
	secretValues, err := h.service.runtimeSecrets(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageConfigure, err)
	}
	if manifest != nil {
		for _, name := range manifest.Secrets {
			if _, ok := secretValues[name]; !ok {
				detection.Warnings = append(detection.Warnings, "configure secret "+name)
				return h.waiting(ctx, appID, deploymentID, detection)
			}
		}
	}
	if err := h.service.repo.SaveDeploymentPlan(ctx, deploymentID, plan); err != nil {
		return nil, h.fail(ctx, deploymentID, StagePlan, err)
	}
	app.Driver = plan.Driver
	app.DesiredState = DesiredRunning
	app.UpdatedAt = time.Now().UTC()
	if err := h.service.repo.Update(ctx, app); err != nil {
		return nil, h.fail(ctx, deploymentID, StagePlan, err)
	}
	if plan.Runtime != nil {
		if err := h.service.repo.SaveRuntime(ctx, *plan.Runtime); err != nil {
			return nil, h.fail(ctx, deploymentID, StagePlan, err)
		}
	}
	workloads, endpoints := materializeTopology(app.ID, plan)
	if err := h.service.repo.StageTopology(ctx, app.ID, workloads, endpoints); err != nil {
		return nil, h.fail(ctx, deploymentID, StagePlan, err)
	}
	persistedWorkloads, err := h.service.repo.Workloads(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StagePlan, err)
	}
	persistedEndpoints, err := h.service.repo.Endpoints(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StagePlan, err)
	}
	// Drivers receive only this plan's services. Older records remain in the
	// inventory for recovery, but must not become synthetic Compose services.
	plannedWorkloads := map[string]bool{}
	for _, w := range plan.Workloads {
		plannedWorkloads[w.Name] = true
	}
	plannedEndpoints := map[string]bool{}
	for _, e := range plan.Endpoints {
		plannedEndpoints[e.Name] = true
	}
	selectedWorkloads := persistedWorkloads[:0]
	for _, w := range persistedWorkloads {
		if plannedWorkloads[w.Name] {
			selectedWorkloads = append(selectedWorkloads, w)
		}
	}
	persistedWorkloads = selectedWorkloads
	selectedEndpoints := persistedEndpoints[:0]
	for _, e := range persistedEndpoints {
		if plannedEndpoints[e.Name] {
			selectedEndpoints = append(selectedEndpoints, e)
		}
	}
	persistedEndpoints = selectedEndpoints
	setStage(StagePrepare)
	setStage(StageBuildOrPull)
	result, err := driver.Deploy(ctx, ExecutionRequest{SensitiveEnvironment: secretValues, Application: app, Source: source, WorkDir: workDir, Deployment: deployment, Workloads: persistedWorkloads, Endpoints: persistedEndpoints}, plan)
	if err != nil {
		stage := StageFailed
		var op *OperationError
		if errors.As(err, &op) && op.Stage != "" {
			stage = op.Stage
		}
		return nil, h.fail(ctx, deploymentID, stage, errors.New(redactSecretValues(err.Error(), secretValues)))
	}
	if err := h.service.repo.ReplaceTopology(ctx, app.ID, persistedWorkloads, persistedEndpoints); err != nil {
		return nil, h.fail(ctx, deploymentID, StageFinalize, err)
	}
	setStage(StageFinalize)
	for name, state := range result.Resources {
		for _, workload := range persistedWorkloads {
			if workload.Name == name {
				_ = h.service.repo.UpdateWorkloadObserved(ctx, workload.ID, state.ResourceID, state.ObservedState, state.HealthState, state.Image)
				break
			}
		}
	}
	for name, port := range result.EndpointPorts {
		for _, endpoint := range persistedEndpoints {
			if endpoint.Name == name {
				value := port
				_ = h.service.repo.UpdateEndpointRuntime(ctx, endpoint.ID, &value, ObservedRunning)
				break
			}
		}
	}
	state, err := h.service.reconcile(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageFinalize, err)
	}
	_ = h.service.repo.FinishDeployment(ctx, deploymentID, "success", StageSuccess, "")
	_ = h.service.repo.Event(ctx, appID, deploymentID, "", "deployment.completed", StageSuccess, "", map[string]any{"status": state.Status})
	return map[string]any{"deployment_id": deploymentID, "status": "success", "application_status": state.Status}, nil
}
func (h *deployJobHandler) waiting(ctx context.Context, appID, deploymentID string, detection DetectionResult) (map[string]any, error) {
	if err := h.service.repo.FinishDeployment(ctx, deploymentID, "waiting_for_configuration", StageWaitingForConfiguration, ""); err != nil {
		return nil, err
	}
	_ = h.service.repo.Event(ctx, appID, deploymentID, "", "deployment.waiting_for_configuration", StageWaitingForConfiguration, "primary endpoint or deployment configuration is ambiguous", map[string]any{"candidates": detection.Endpoints, "warnings": detection.Warnings})
	return map[string]any{"deployment_id": deploymentID, "status": "waiting_for_configuration", "detection": detection}, nil
}
func (h *deployJobHandler) fail(ctx context.Context, deploymentID, stage string, err error) error {
	status := "failed"
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		status = "cancelled"
		stage = StageCancelled
	}
	_ = h.service.repo.FinishDeployment(context.Background(), deploymentID, status, stage, err.Error())
	deployment, _ := h.service.repo.Deployment(context.Background(), deploymentID)
	if deployment.ApplicationID != "" {
		_ = h.service.repo.Event(context.Background(), deployment.ApplicationID, deploymentID, "", "deployment."+status, stage, err.Error(), nil)
	}
	return err
}

type lifecycleJobHandler struct {
	service *Service
	jobType string
	logger  JobLogger
}

func NewLifecycleJobHandler(service *Service, logger JobLogger, jobType string) jobs.Handler {
	return &lifecycleJobHandler{service: service, logger: logger, jobType: jobType}
}
func (h *lifecycleJobHandler) Type() string { return h.jobType }
func (h *lifecycleJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	appID, _ := job.Payload["application_id"].(string)
	if appID == "" {
		return nil, fmt.Errorf("%w: application_id", ErrInvalidInput)
	}
	app, err := h.service.repo.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	source, err := h.service.repo.Source(ctx, appID)
	if err != nil {
		return nil, err
	}
	workloads, err := h.service.repo.Workloads(ctx, appID)
	if err != nil {
		return nil, err
	}
	endpoints, err := h.service.repo.Endpoints(ctx, appID)
	if err != nil {
		return nil, err
	}
	if h.jobType == JobReconcile {
		state, err := h.service.reconcile(ctx, appID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": state.Status, "observed_state": state.ObservedState, "health_state": state.HealthState}, nil
	}
	if h.jobType == JobRemove {
		return h.remove(ctx, app, source, workloads, endpoints, job)
	}
	driver, ok := h.service.drivers.Get(app.Driver)
	if !ok {
		return nil, fmt.Errorf("%w: driver %q", ErrProviderUnavailable, app.Driver)
	}
	request := InspectRequest{Application: app, Source: source, WorkDir: h.service.workDir(app, source), Workloads: workloads, Endpoints: endpoints}
	switch h.jobType {
	case JobStart:
		app.DesiredState = DesiredRunning
		app.UpdatedAt = time.Now().UTC()
		if err := h.service.repo.Update(ctx, app); err != nil {
			return nil, err
		}
		err = driver.Start(ctx, request)
	case JobStop:
		app.DesiredState = DesiredStopped
		app.UpdatedAt = time.Now().UTC()
		if err := h.service.repo.Update(ctx, app); err != nil {
			return nil, err
		}
		err = driver.Stop(ctx, request)
	case JobRestart:
		app.DesiredState = DesiredRunning
		if err := h.service.repo.Update(ctx, app); err != nil {
			return nil, err
		}
		err = driver.Restart(ctx, request)
	default:
		return nil, fmt.Errorf("%w: lifecycle job type", ErrInvalidInput)
	}
	if err != nil {
		return nil, err
	}
	state, err := h.service.reconcile(ctx, appID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": state.Status, "observed_state": state.ObservedState, "health_state": state.HealthState}, nil
}

func materializeTopology(applicationID string, plan DeploymentPlan) ([]Workload, []Endpoint) {
	now := time.Now().UTC()
	workloads := make([]Workload, 0, len(plan.Workloads))
	ids := map[string]string{}
	for _, item := range plan.Workloads {
		id := NewID()
		ids[item.Name] = id
		workloads = append(workloads, Workload{ID: id, ApplicationID: applicationID, Name: item.Name, Role: item.Role, Image: item.Image, DesiredState: DesiredRunning, ObservedState: ObservedUnknown, HealthState: HealthUnknown, Primary: item.Primary, Metadata: item.Metadata, CreatedAt: now, UpdatedAt: now})
	}
	endpoints := make([]Endpoint, 0, len(plan.Endpoints))
	for _, item := range plan.Endpoints {
		// HostPort records the observed mapping, not the requested replacement.
		// ReplaceTopology preserves the previous mapping until Deploy succeeds.
		var host *int
		var domain *string
		if item.Domain != "" {
			v := item.Domain
			domain = &v
		}
		endpoints = append(endpoints, Endpoint{ID: NewID(), ApplicationID: applicationID, WorkloadID: ids[item.Workload], Name: item.Name, Protocol: item.Protocol, ContainerPort: item.ContainerPort, HostPort: host, Domain: domain, Public: item.Public, Primary: item.Primary, TLSMode: item.TLSMode, HealthPath: item.HealthPath, Status: ObservedUnknown, CreatedAt: now, UpdatedAt: now})
	}
	return workloads, endpoints
}

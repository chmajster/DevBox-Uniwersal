package applications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	if relativeRoot, _ := input.Configuration["root_dir"].(string); strings.TrimSpace(relativeRoot) != "" {
		base, baseErr := filepath.EvalSymlinks(workDir)
		root, rootErr := filepath.EvalSymlinks(filepath.Join(workDir, filepath.FromSlash(relativeRoot)))
		rel, relErr := filepath.Rel(base, root)
		info, statErr := os.Stat(root)
		if baseErr != nil || rootErr != nil || relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || statErr != nil || !info.IsDir() {
			return nil, fmt.Errorf("%w: root_dir must name a directory inside the Git source", ErrInvalidInput)
		}
		workDir = root
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
	jobType string
	service *Service
	logger  JobLogger
}

func NewDeployJobHandler(service *Service, logger JobLogger) jobs.Handler {
	return &deployJobHandler{service: service, logger: logger}
}
func NewDeploymentActionHandler(service *Service, logger JobLogger, jobType string) jobs.Handler {
	return &deployJobHandler{service: service, logger: logger, jobType: jobType}
}
func (h *deployJobHandler) Type() string {
	if h.jobType != "" {
		return h.jobType
	}
	return JobDeploy
}
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
			_ = h.logger.Log(ctx, job.ID, "info", "application.deployment.stage", map[string]any{"stage": stage, "progress": stageProgress(stage), "application_id": appID, "deployment_id": deploymentID})
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
	config := decodeConfiguration(app.SourceConfig)
	previousDriverName := strings.TrimSpace(app.Driver)
	selectedDriver := app.Driver
	if pending, ok := config["deployment_driver"].(string); ok {
		selectedDriver = pending
	}
	previousWorkloads, err := h.service.repo.Workloads(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageSource, err)
	}
	previousEndpoints, err := h.service.repo.Endpoints(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageSource, err)
	}
	hasActiveResources := false
	for _, workload := range previousWorkloads {
		if workload.DriverResourceID != "" {
			hasActiveResources = true
			break
		}
	}
	if relativeRoot, _ := config["root_dir"].(string); strings.TrimSpace(relativeRoot) != "" && (app.SourceType == SourceGit || app.SourceType == SourceEmpty) {
		relativeRoot = strings.TrimSpace(relativeRoot)
		base, baseErr := filepath.EvalSymlinks(workDir)
		root, rootErr := filepath.EvalSymlinks(filepath.Join(workDir, filepath.FromSlash(relativeRoot)))
		rel, relErr := filepath.Rel(base, root)
		info, statErr := os.Stat(root)
		if baseErr != nil || rootErr != nil || relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || statErr != nil || !info.IsDir() {
			return nil, h.fail(ctx, deploymentID, StageSource, fmt.Errorf("%w: root_dir must name a directory inside the application source", ErrInvalidInput))
		}
		workDir = root
	}
	source.CurrentRevision = revision
	_ = h.service.repo.UpdateSource(ctx, source)
	setStage(StageDetect)
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
	detection, detectErr := h.service.selector.Detect(ctx, DetectRequest{SourceType: app.SourceType, Source: source, WorkDir: workDir, Configuration: config}, selectedDriver)
	if detectErr != nil && !errors.Is(detectErr, ErrConfigurationRequired) {
		return nil, h.fail(ctx, deploymentID, StageDetect, detectErr)
	}
	if detection.Driver == "" {
		if selectedDriver != "" {
			detection.Driver = selectedDriver
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
	if h.Type() == JobBuild {
		secretValues, err := h.service.runtimeSecrets(ctx, appID)
		if err != nil {
			return nil, h.fail(ctx, deploymentID, StageConfigure, err)
		}
		builder, ok := driver.(interface {
			Build(context.Context, ExecutionRequest, DeploymentPlan) error
		})
		if !ok {
			return nil, h.fail(ctx, deploymentID, StageBuildOrPull, ErrProviderUnavailable)
		}
		setStage(StageBuildOrPull)
		buildCtx := jobs.WithOutput(ctx, func(stream, line string) {
			if h.logger != nil {
				_ = h.logger.Log(ctx, job.ID, "info", "application.process.output", map[string]any{"stream": stream, "output": redactLogLine(redactSecretValues(line, secretValues))})
			}
		})
		if err := builder.Build(buildCtx, ExecutionRequest{Application: app, Source: source, WorkDir: workDir}, plan); err != nil {
			return nil, h.fail(ctx, deploymentID, StageBuildOrPull, errors.New(redactSecretValues(err.Error(), secretValues)))
		}
		_ = h.service.repo.SaveDeploymentPlan(ctx, deploymentID, plan)
		_ = h.service.repo.FinishDeployment(ctx, deploymentID, "success", StageSuccess, "")
		return map[string]any{"deployment_id": deploymentID, "status": "success", "progress": 100, "stage": StageSuccess}, nil
	}
	if err := h.service.repo.SaveDeploymentPlan(ctx, deploymentID, plan); err != nil {
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
	profile := ""
	boundDatabase := false
	if h.service.databaseResolver != nil {
		connection, password, found, resolveErr := h.service.databaseResolver.ResolveBoundApplicationDatabase(ctx, appID)
		if resolveErr != nil {
			return nil, h.fail(ctx, deploymentID, StageConfigure, errors.New("could not resolve application database credentials/grants"))
		}
		boundDatabase = found
		if found {
			secretValues["DB_HOST"] = connection.Host
			secretValues["DB_PORT"] = fmt.Sprint(connection.Port)
			secretValues["DB_NAME"], secretValues["DB_DATABASE"] = connection.Database, connection.Database
			secretValues["DB_USER"], secretValues["DB_USERNAME"] = connection.Username, connection.Username
			secretValues["DB_PASSWORD"] = string(password)
			secretValues["DB_DRIVER"] = connection.Engine
		}
		clear(password)
	}
	if plan.Runtime != nil {
		profile, _ = plan.Runtime.Metadata["profile"].(string)
	}
	if profile == "wordpress" {
		existingConfig := regularFile(filepath.Join(workDir, "wp-config.php"))
		publicEnvironment := map[string]string{}
		if len(plan.Workloads) > 0 {
			publicEnvironment = plan.Workloads[0].Environment
		}
		databaseEnvironment := func(key string) string {
			if secretValues[key] != "" {
				return secretValues[key]
			}
			return publicEnvironment[key]
		}
		suppliedDatabase := databaseEnvironment("WORDPRESS_DB_HOST") != "" && databaseEnvironment("WORDPRESS_DB_NAME") != "" && databaseEnvironment("WORDPRESS_DB_USER") != ""
		found := boundDatabase
		if h.service.databaseResolver != nil {
			connection, password, bound, resolveErr := h.service.databaseResolver.ResolveBoundApplicationDatabase(ctx, appID)
			if resolveErr != nil {
				return nil, h.fail(ctx, deploymentID, StageConfigure, errors.New("could not resolve the WordPress database binding"))
			}
			found = bound
			if found {
				if connection.Host == "" || connection.Port < 1 || connection.Port > 65535 || connection.Database == "" || connection.Username == "" {
					return nil, h.fail(ctx, deploymentID, StageConfigure, errors.New("WordPress database binding is incomplete"))
				}
				secretValues["WORDPRESS_DB_HOST"] = net.JoinHostPort(connection.Host, fmt.Sprint(connection.Port))
				secretValues["WORDPRESS_DB_NAME"] = connection.Database
				secretValues["WORDPRESS_DB_USER"] = connection.Username
				secretValues["WORDPRESS_DB_PASSWORD"] = string(password)
			}
			for i := range password {
				password[i] = 0
			}
		}
		if !existingConfig && !found && !suppliedDatabase {
			return h.waiting(ctx, appID, deploymentID, DetectionResult{Profile: "wordpress", Warnings: []string{"select an existing MySQL/MariaDB database or provide WORDPRESS_DB_HOST, WORDPRESS_DB_NAME and WORDPRESS_DB_USER with the database password in SecretStore"}})
		}
	}

	if err := h.service.repo.SaveDeploymentPlan(ctx, deploymentID, plan); err != nil {
		return nil, h.fail(ctx, deploymentID, StagePlan, err)
	}
	if !hasActiveResources {
		app.Driver = plan.Driver
	}
	app.DesiredState = DesiredRunning
	app.UpdatedAt = time.Now().UTC()
	if err := h.service.repo.Update(ctx, app); err != nil {
		return nil, h.fail(ctx, deploymentID, StagePlan, err)
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
	var previousDriver DeploymentDriver
	var previousRequest InspectRequest
	previousDriverStopped := false
	if previousDriverName != "" && previousDriverName != plan.Driver && hasActiveResources {
		previousDriver, ok = h.service.drivers.Get(previousDriverName)
		if !ok {
			return nil, h.fail(ctx, deploymentID, StagePrepare, fmt.Errorf("%w: previous driver %q is unavailable", ErrProviderUnavailable, previousDriverName))
		}
		previousRequest = InspectRequest{Application: app, Source: source, WorkDir: workDir, Workloads: previousWorkloads, Endpoints: previousEndpoints}
		if err := previousDriver.Stop(ctx, previousRequest); err != nil {
			startErr := restorePreviousDriver(previousDriver, previousRequest)
			if startErr != nil {
				err = fmt.Errorf("stop previous driver before switch: %v; restore previous services: %v", err, startErr)
			} else {
				err = fmt.Errorf("stop previous driver before switch: %w", err)
			}
			return nil, h.fail(ctx, deploymentID, StagePrepare, err)
		}
		previousDriverStopped = true
	}
	setStage(StageBuildOrPull)
	executionCtx := jobs.WithOutput(ctx, func(stream, line string) {
		if h.logger != nil {
			_ = h.logger.Log(ctx, job.ID, "info", "application.process.output", map[string]any{"stream": stream, "output": redactLogLine(redactSecretValues(line, secretValues))})
		}
	})
	result, err := driver.Deploy(executionCtx, ExecutionRequest{ForceBuild: h.Type() == JobRebuild, Recreate: h.Type() == JobRecreate, Progress: func(stage string, progress int) { setStage(stage) }, SensitiveEnvironment: secretValues, Application: app, Source: source, WorkDir: workDir, Deployment: deployment, Workloads: persistedWorkloads, Endpoints: persistedEndpoints}, plan)
	if err != nil {
		if previousDriverStopped {
			if restoreErr := restorePreviousDriver(previousDriver, InspectRequest{Application: app, Source: source, WorkDir: workDir, Workloads: previousWorkloads, Endpoints: previousEndpoints}); restoreErr != nil {
				err = fmt.Errorf("%v; previous driver restore failed: %v", err, restoreErr)
			}
		}
		if plan.Driver == "managed" && hasActiveResources {
			_ = h.service.repo.ReplaceTopology(context.Background(), app.ID, previousWorkloads, previousEndpoints)
		}
		if plan.Driver == "compose" && previousDriverName == "compose" && hasActiveResources {
			if restoreErr := h.restoreCompose(app, source, deploymentID, workDir, previousWorkloads, previousEndpoints, secretValues); restoreErr != nil {
				err = fmt.Errorf("%v; previous Compose configuration restore failed: %v", err, restoreErr)
			}
		}
		stage := StageFailed
		var op *OperationError
		if errors.As(err, &op) && op.Stage != "" {
			stage = op.Stage
		}
		if errors.As(err, &op) && op.BuildLog != "" && h.logger != nil {
			output := redactSecretValues(op.BuildLog, secretValues)
			_ = h.logger.Log(ctx, job.ID, "info", "application.build.output", map[string]any{
				"output": output, "application_id": appID, "deployment_id": deploymentID,
			})
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
	app.Driver = plan.Driver
	app.DesiredState = DesiredRunning
	app.UpdatedAt = time.Now().UTC()
	storedConfig := decodeConfiguration(app.SourceConfig)
	delete(storedConfig, "deployment_driver")
	app.SourceConfig, err = json.Marshal(storedConfig)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageFinalize, err)
	}
	if err := h.service.repo.Update(ctx, app); err != nil {
		return nil, h.fail(ctx, deploymentID, StageFinalize, err)
	}
	if plan.Runtime != nil {
		if err := h.service.repo.SaveRuntime(ctx, *plan.Runtime); err != nil {
			return nil, h.fail(ctx, deploymentID, StageFinalize, err)
		}
	} else if err := h.service.repo.DeleteRuntime(ctx, appID); err != nil {
		return nil, h.fail(ctx, deploymentID, StageFinalize, err)
	}
	warnings := []string{}
	if previousDriverStopped {
		previousWorkloads = workloadsNotReused(previousWorkloads, result.Resources)
		if len(previousWorkloads) > 0 {
			previousRequest.Workloads = previousWorkloads
			if err := previousDriver.Remove(ctx, previousRequest); err != nil {
				warnings = append(warnings, "previous driver resources could not be removed: "+err.Error())
			}
		}
	}
	state, err := h.service.reconcile(ctx, appID)
	if err != nil {
		return nil, h.fail(ctx, deploymentID, StageFinalize, err)
	}
	if state.Status != "running" {
		return nil, h.fail(ctx, deploymentID, StageHealthcheck, fmt.Errorf("application failed readiness: %s; inspect application logs", state.Status))
	}
	setStage(StageRouting)
	if err := h.service.syncRoutes(ctx, appID); err != nil {
		return nil, h.fail(ctx, deploymentID, StageRouting, err)
	}

	_ = h.service.repo.FinishDeployment(ctx, deploymentID, "success", StageSuccess, "")
	_ = h.service.repo.Event(ctx, appID, deploymentID, "", "deployment.completed", StageSuccess, "", map[string]any{"status": state.Status, "warnings": warnings})
	return map[string]any{"deployment_id": deploymentID, "status": "success", "application_status": state.Status, "warnings": warnings, "progress": 100, "stage": StageSuccess}, nil
}
func (h *deployJobHandler) waiting(ctx context.Context, appID, deploymentID string, detection DetectionResult) (map[string]any, error) {
	if err := h.service.repo.FinishDeployment(ctx, deploymentID, "waiting_for_configuration", StageWaitingForConfiguration, ""); err != nil {
		return nil, err
	}
	_ = h.service.repo.Event(ctx, appID, deploymentID, "", "deployment.waiting_for_configuration", StageWaitingForConfiguration, "primary endpoint or deployment configuration is ambiguous", map[string]any{"candidates": detection.Endpoints, "warnings": detection.Warnings})
	return map[string]any{"deployment_id": deploymentID, "status": "waiting_for_configuration", "detection": detection}, nil
}

func workloadsNotReused(previous []Workload, current map[string]ResourceState) []Workload {
	out := make([]Workload, 0, len(previous))
	for _, workload := range previous {
		if workload.DriverResourceID == "" {
			continue
		}
		if replacement, ok := current[workload.Name]; ok && replacement.ResourceID == workload.DriverResourceID {
			continue
		}
		out = append(out, workload)
	}
	return out
}

func restorePreviousDriver(driver DeploymentDriver, request InspectRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return driver.Start(ctx, request)
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
		workloads, _ := h.service.repo.Workloads(context.Background(), deployment.ApplicationID)
		resources := false
		for _, w := range workloads {
			if w.DriverResourceID != "" {
				resources = true
			}
		}
		if !resources && status == "failed" {
			_ = h.service.repo.UpdateApplicationState(context.Background(), deployment.ApplicationID, ObservedFailed, HealthUnhealthy)
		}
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
	secretValues, err := h.service.runtimeSecrets(ctx, appID)
	if err != nil {
		return nil, err
	}
	if h.service.databaseResolver != nil {
		_, password, found, err := h.service.databaseResolver.ResolveBoundApplicationDatabase(ctx, appID)
		if err != nil {
			return nil, err
		}
		if found {
			secretValues["DB_PASSWORD"] = string(password)
		}
		clear(password)
	}
	ctx = jobs.WithOutput(ctx, func(stream, line string) {
		if h.logger != nil {
			_ = h.logger.Log(ctx, job.ID, "info", "application.process.output", map[string]any{"stream": stream, "output": redactLogLine(redactSecretValues(line, secretValues)), "stage": StageStart, "progress": 50})
		}
	})
	request := InspectRequest{Application: app, Source: source, WorkDir: h.service.workDir(app, source), Workloads: workloads, Endpoints: endpoints}
	switch h.jobType {
	case JobPull:
		provider, ok := driver.(interface {
			Pull(context.Context, InspectRequest) error
		})
		if !ok {
			return nil, fmt.Errorf("%w: pull is available for Existing Compose", ErrInvalidInput)
		}
		err = provider.Pull(ctx, request)
	case JobDown:
		app.DesiredState = DesiredStopped
		if err := h.service.repo.Update(ctx, app); err != nil {
			return nil, err
		}
		err = driver.Remove(ctx, request)
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
	if h.logger != nil {
		_ = h.logger.Log(ctx, job.ID, "info", "application.stage", map[string]any{"stage": StageHealthcheck, "progress": 85})
	}
	state, err := h.service.reconcile(ctx, appID)
	if err != nil {
		return nil, err
	}
	if h.jobType == JobStart || h.jobType == JobRestart {
		readiness, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()
		stable := time.Time{}
		for {
			if state.Status == "failed" && state.ObservedState != ObservedRunning && state.ObservedState != ObservedStarting {
				lines, _ := h.service.Logs(ctx, appID, 50, "")
				output := []string{}
				for _, line := range lines {
					output = append(output, line.Line)
				}
				return nil, fmt.Errorf("application failed readiness after %s; logs: %s", h.jobType, strings.Join(output, "\n"))
			}
			if state.Status == "running" {
				if stable.IsZero() {
					stable = time.Now()
				}
				if time.Since(stable) >= time.Second {
					break
				}
			} else {
				stable = time.Time{}
			}
			select {
			case <-readiness.Done():
				return nil, fmt.Errorf("healthcheck timeout: application is %s: %w", state.Status, readiness.Err())
			case <-time.After(250 * time.Millisecond):
			}
			state, err = h.service.reconcile(readiness, appID)
			if err != nil {
				return nil, err
			}
		}
		if err := h.service.syncRoutes(ctx, appID); err != nil {
			return nil, err
		}
	}
	if h.logger != nil {
		_ = h.logger.Log(ctx, job.ID, "info", "application.stage", map[string]any{"stage": StageSuccess, "progress": 100})
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

func stageProgress(stage string) int {
	stages := map[string]int{StageSource: 5, StageDetect: 10, StagePlan: 15, StagePrepare: 20, StageDependencies: 25, StageBuildOrPull: 35, StageCreate: 65, StageNetwork: 70, StageStart: 75, StageHealthcheck: 85, StageDiscover: 90, StageRouting: 95, StageFinalize: 98, StageSuccess: 100}
	return stages[stage]
}

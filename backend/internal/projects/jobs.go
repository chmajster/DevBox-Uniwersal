package projects

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	jobpkg "github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
)

const (
	JobClone    = "project.git.clone"
	JobFetch    = "project.git.fetch"
	JobPull     = "project.git.pull"
	JobCheckout = "project.git.checkout"
	JobDeploy   = "project.deploy"
)

type jobLogger interface {
	Log(ctx context.Context, jobID, level, message string, fields map[string]any) error
}

type GitJobHandler struct {
	typeName string
	repo     *Repository
	git      *GitClient
	logger   jobLogger
}

func NewGitJobHandler(typeName string, repo *Repository, git *GitClient, logger jobLogger) *GitJobHandler {
	return &GitJobHandler{typeName: typeName, repo: repo, git: git, logger: logger}
}
func (h *GitJobHandler) Type() string { return h.typeName }
func (h *GitJobHandler) Run(ctx context.Context, job domain.Job) (map[string]any, error) {
	projectID, err := payloadString(job.Payload, "project_id")
	if err != nil {
		return nil, err
	}
	p, err := h.repo.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	cred := ProjectCredentialRef(p)
	log := func(message string) {
		_ = h.logger.Log(ctx, job.ID, "info", message, map[string]any{"project_id": p.ID})
	}
	switch h.typeName {
	case JobClone:
		if p.SourceType != SourceGit {
			return nil, errors.New("clone is only valid for Git projects")
		}
		if h.git.IsRepository(ctx, p.LocalPath) {
			return nil, errors.New("destination already contains a Git repository")
		}
		if err := os.MkdirAll(filepath.Dir(p.LocalPath), 0o750); err != nil {
			return nil, err
		}
		log("git.clone.start")
		if err := h.git.Clone(ctx, providers.GitSource{RepositoryURL: p.RepositoryURL, Reference: p.Branch, Destination: p.LocalPath, CredentialRef: cred}); err != nil {
			_ = h.repo.UpdateStatus(ctx, p.ID, "failed")
			return nil, err
		}
	case JobFetch:
		log("git.fetch.start")
		if err := h.git.Fetch(ctx, p.LocalPath, cred); err != nil {
			return nil, err
		}
	case JobPull:
		log("git.pull.start")
		if err := h.git.PullWithCredential(ctx, p.LocalPath, cred); err != nil {
			return nil, err
		}
	case JobCheckout:
		branch, err := payloadString(job.Payload, "branch")
		if err != nil {
			return nil, err
		}
		log("git.checkout.start")
		if err := h.git.Checkout(ctx, p.LocalPath, branch); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported Git job %s", h.typeName)
	}
	state, err := h.git.State(ctx, p.LocalPath)
	if err != nil {
		return nil, err
	}
	if err := h.repo.UpdateGitState(ctx, p.ID, state.Branch, state.Commit); err != nil {
		return nil, err
	}
	if h.typeName == JobClone {
		_ = h.repo.UpdateStatus(ctx, p.ID, "ready")
	}
	log("git.operation.success")
	return map[string]any{"branch": state.Branch, "commit": state.Commit, "dirty": state.Dirty, "ahead": state.Ahead, "behind": state.Behind}, nil
}

type ProjectRouteManager interface {
	EnsureProjectRoute(ctx context.Context, projectID, hostname string, targetPort int) error
}

type ComposeDeployer interface {
	Available(ctx context.Context) error
	ComposeValidate(ctx context.Context, directory, projectName string) error
	ComposePull(ctx context.Context, directory, projectName, service string) error
	ComposeBuild(ctx context.Context, directory, projectName, service string) error
	ComposeUp(ctx context.Context, directory, projectName, service string) error
	ComposeDown(ctx context.Context, directory, projectName string) error
	ComposeHealthy(ctx context.Context, directory, projectName string) error
}

type ManagedContainerDeployer interface {
	Available(ctx context.Context) error
	ManagedImageExists(ctx context.Context, image string) (bool, error)
	BuildManaged(ctx context.Context, spec containerspec.DeploymentSpec) error
	ReplaceManaged(ctx context.Context, spec containerspec.DeploymentSpec) error
}

type DeploymentIntegrations struct {
	Ports   providers.PortAllocator
	Routes  ProjectRouteManager
	Compose ComposeDeployer
	Managed ManagedContainerDeployer
}

type DeploymentHandler struct {
	repo         *Repository
	git          *GitClient
	runtimes     runtimes.Registry
	logger       jobLogger
	integrations DeploymentIntegrations
}

func NewDeploymentHandler(repo *Repository, git *GitClient, registry runtimes.Registry, logger jobLogger, integrations ...DeploymentIntegrations) *DeploymentHandler {
	handler := &DeploymentHandler{repo: repo, git: git, runtimes: registry, logger: logger}
	if len(integrations) > 0 {
		handler.integrations = integrations[0]
	}
	return handler
}

func (h *DeploymentHandler) Type() string { return JobDeploy }

func (h *DeploymentHandler) Run(ctx context.Context, job domain.Job) (result map[string]any, runErr error) {
	projectID, err := payloadString(job.Payload, "project_id")
	if err != nil {
		return nil, err
	}
	deploymentID, err := payloadString(job.Payload, "deployment_id")
	if err != nil {
		return nil, err
	}
	p, err := h.repo.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	reconcile := payloadBool(job.Payload, "reconcile")
	forceRebuild := payloadBool(job.Payload, "force_rebuild")

	started := time.Now().UTC()
	commitBefore := p.CurrentCommit
	if h.git.IsRepository(ctx, p.LocalPath) {
		if revision, revErr := h.git.Revision(ctx, p.LocalPath); revErr == nil {
			commitBefore = revision
		}
	}
	if err := h.repo.StartDeployment(ctx, deploymentID, DeploymentPreparing, commitBefore, started); err != nil {
		return nil, err
	}
	_ = h.repo.UpdateStatus(ctx, p.ID, "deploying")

	currentStage := DeploymentPreparing
	commitAfter := commitBefore
	var (
		allocatedPort  int
		composeStarted bool
		composeDir     string
		composeName    string
	)
	defer func() {
		if runErr == nil {
			return
		}
		if composeStarted && h.integrations.Compose != nil {
			_ = h.integrations.Compose.ComposeDown(context.Background(), composeDir, composeName)
		}
		if allocatedPort > 0 && h.integrations.Ports != nil {
			_ = h.integrations.Ports.Release(context.Background(), allocatedPort)
		}
		finished := time.Now().UTC()
		_ = h.repo.FinishDeployment(context.Background(), deploymentID, DeploymentFailed, DeploymentFailed, commitAfter, runErr.Error(), finished, finished.Sub(started))
		_ = h.repo.UpdateStatus(context.Background(), p.ID, "failed")
		_ = h.logger.Log(context.Background(), job.ID, "error", "deployment.failed", map[string]any{"stage": currentStage, "error": runErr.Error()})
	}()

	setStage := func(next string) error {
		if !validDeploymentTransition(currentStage, next) {
			return fmt.Errorf("invalid deployment transition %s -> %s", currentStage, next)
		}
		if err := h.repo.SetDeploymentStage(ctx, deploymentID, next); err != nil {
			return err
		}
		currentStage = next
		_ = h.logger.Log(ctx, job.ID, "info", "deployment.stage", map[string]any{"stage": next})
		return nil
	}

	workDir, err := SafeWorkingDirectory(p.LocalPath, p.WorkingDirectory)
	if err != nil {
		return nil, err
	}

	if err := setStage(DeploymentUpdatingSource); err != nil {
		return nil, err
	}
	if p.SourceType == SourceGit {
		if !h.git.IsRepository(ctx, p.LocalPath) {
			return nil, errors.New("provider unavailable: Git repository is not cloned")
		}
		if !reconcile {
			if err := h.git.PullWithCredential(ctx, p.LocalPath, ProjectCredentialRef(p)); err != nil {
				return nil, err
			}
		}
	}
	if h.git.IsRepository(ctx, p.LocalPath) {
		commitAfter, err = h.git.Revision(ctx, p.LocalPath)
		if err != nil {
			return nil, err
		}
		if state, stateErr := h.git.State(ctx, p.LocalPath); stateErr == nil {
			_ = h.repo.UpdateGitState(ctx, p.ID, state.Branch, state.Commit)
		}
	}

	config, err := h.repo.RuntimeContainerConfig(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if config.ContainerPolicy == "" {
		config.ContainerPolicy = ContainerPolicyAuto
	}
	hasCompose := projectHasCompose(workDir)
	hasDockerfile := projectHasDockerfile(workDir)
	if config.ContainerPolicy == ContainerPolicyCustom && !hasCompose && !hasDockerfile {
		return nil, errors.New("custom container policy requires compose.yaml/docker-compose.yml or Dockerfile")
	}

	// Project-owned Compose takes precedence. There is no host-runtime fallback.
	if hasCompose {
		if h.integrations.Compose == nil {
			return nil, errors.New("provider unavailable: docker compose")
		}
		if err := h.integrations.Compose.Available(ctx); err != nil {
			return nil, fmt.Errorf("provider unavailable: docker: %w", err)
		}
		composeDir = workDir
		composeName = p.Slug
		if err := h.integrations.Compose.ComposeValidate(ctx, composeDir, composeName); err != nil {
			return nil, fmt.Errorf("docker compose validation: %w", err)
		}
		if err := setStage(DeploymentDependencies); err != nil {
			return nil, err
		}
		if !reconcile || forceRebuild {
			if err := h.integrations.Compose.ComposePull(ctx, composeDir, composeName, ""); err != nil {
				return nil, fmt.Errorf("docker compose pull: %w", err)
			}
		}
		if err := setStage(DeploymentBuilding); err != nil {
			return nil, err
		}
		if !reconcile || forceRebuild {
			if err := h.integrations.Compose.ComposeBuild(ctx, composeDir, composeName, ""); err != nil {
				return nil, fmt.Errorf("docker compose build: %w", err)
			}
		}
		if err := setStage(DeploymentStarting); err != nil {
			return nil, err
		}
		if err := h.integrations.Compose.ComposeUp(ctx, composeDir, composeName, ""); err != nil {
			return nil, fmt.Errorf("docker compose up: %w", err)
		}
		composeStarted = true
		if err := setStage(DeploymentHealthcheck); err != nil {
			return nil, err
		}
		if err := h.integrations.Compose.ComposeHealthy(ctx, composeDir, composeName); err != nil {
			return nil, fmt.Errorf("docker compose healthcheck: %w", err)
		}

		targetPort := 0
		if p.Port != nil {
			targetPort = *p.Port
		} else if parsed, ok := healthcheckPort(p.Healthcheck); ok {
			targetPort = parsed
		}
		if h.integrations.Routes != nil {
			if targetPort == 0 {
				return nil, errors.New("compose deployment is healthy but no host target port is configured for reverse proxy")
			}
			if err := h.integrations.Routes.EnsureProjectRoute(ctx, p.ID, routeHostname(p), targetPort); err != nil {
				return nil, fmt.Errorf("reverse proxy: %w", err)
			}
		}
		return h.finishSuccess(ctx, deploymentID, p.ID, commitBefore, commitAfter, started, setStage)
	}

	if h.integrations.Managed == nil {
		return nil, errors.New("provider unavailable: managed docker runtime")
	}
	if err := h.integrations.Managed.Available(ctx); err != nil {
		return nil, fmt.Errorf("provider unavailable: docker: %w", err)
	}

	port := 0
	if p.Port != nil && *p.Port > 0 {
		port = *p.Port
	} else {
		if h.integrations.Ports == nil {
			return nil, errors.New("provider unavailable: port allocator")
		}
		lease, reserveErr := h.integrations.Ports.Reserve(ctx, p.ID, "application", nil)
		if reserveErr != nil {
			return nil, fmt.Errorf("allocate port: %w", reserveErr)
		}
		port = lease.Port
		allocatedPort = port
	}

	var spec containerspec.DeploymentSpec
	if hasDockerfile {
		spec, err = containerspec.GenerateCustomDockerfile(p.ID, workDir, port)
		if err != nil {
			return nil, fmt.Errorf("custom Dockerfile: %w", err)
		}
	} else {
		runtimeName := strings.TrimSpace(config.Runtime)
		if runtimeName == "" {
			runtimeName = strings.TrimSpace(p.Runtime)
		}
		if runtimeName == "" {
			runtimeName, err = h.detectRuntime(ctx, p, workDir)
			if err != nil {
				return nil, err
			}
			if err := h.repo.UpdateRuntime(ctx, p.ID, runtimeName); err != nil {
				return nil, err
			}
		}
		modules := make([]containerspec.Module, 0, len(config.Modules))
		for _, module := range config.Modules {
			modules = append(modules, containerspec.Module{Name: module.Name, Version: module.Version})
		}
		spec, err = containerspec.GenerateManaged(p.ID, workDir, runtimeName, config.RuntimeVersion, modules, commitAfter, port)
		if err != nil {
			return nil, fmt.Errorf("managed runtime specification: %w", err)
		}
	}

	state, err := h.repo.RuntimeContainerState(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	imageExists, err := h.integrations.Managed.ManagedImageExists(ctx, spec.Image)
	if err != nil {
		return nil, fmt.Errorf("inspect managed image: %w", err)
	}
	needsBuild := forceRebuild || state.Fingerprint != spec.Fingerprint || state.ImageTag != spec.Image || !imageExists

	if err := setStage(DeploymentDependencies); err != nil {
		return nil, err
	}
	if err := setStage(DeploymentBuilding); err != nil {
		return nil, err
	}
	if needsBuild {
		_ = h.logger.Log(ctx, job.ID, "info", "runtime.container.build", map[string]any{
			"runtime": spec.Runtime, "version": spec.Version, "image": spec.Image, "fingerprint": spec.Fingerprint,
		})
		if err := h.integrations.Managed.BuildManaged(ctx, spec); err != nil {
			return nil, err
		}
	} else {
		_ = h.logger.Log(ctx, job.ID, "info", "runtime.container.build.skipped", map[string]any{"image": spec.Image, "fingerprint": spec.Fingerprint})
	}

	if err := setStage(DeploymentStarting); err != nil {
		return nil, err
	}
	if err := h.integrations.Managed.ReplaceManaged(ctx, spec); err != nil {
		return nil, fmt.Errorf("replace managed container: %w", err)
	}
	if err := setStage(DeploymentHealthcheck); err != nil {
		return nil, err
	}
	if err := h.repo.SaveRuntimeContainerState(ctx, p.ID, RuntimeContainerState{
		ContainerName: spec.ContainerName,
		ImageTag: spec.Image,
		Fingerprint: spec.Fingerprint,
	}); err != nil {
		return nil, err
	}
	if h.integrations.Routes != nil {
		if err := h.integrations.Routes.EnsureProjectRoute(ctx, p.ID, routeHostname(p), port); err != nil {
			return nil, fmt.Errorf("reverse proxy: %w", err)
		}
	}

	allocatedPort = 0
	return h.finishSuccess(ctx, deploymentID, p.ID, commitBefore, commitAfter, started, setStage)
}

func projectHasDockerfile(workDir string) bool {
	info, err := os.Stat(filepath.Join(workDir, "Dockerfile"))
	return err == nil && info.Mode().IsRegular()
}

func projectHasCompose(workDir string) bool {
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"} {
		info, err := os.Stat(filepath.Join(workDir, name))
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func (h *DeploymentHandler) finishSuccess(ctx context.Context, deploymentID, projectID, commitBefore, commitAfter string, started time.Time, setStage func(string) error) (map[string]any, error) {
	if err := setStage(DeploymentSuccess); err != nil {
		return nil, err
	}
	finished := time.Now().UTC()
	if err := h.repo.FinishDeployment(ctx, deploymentID, DeploymentSuccess, DeploymentSuccess, commitAfter, "", finished, finished.Sub(started)); err != nil {
		return nil, err
	}
	_ = h.repo.UpdateStatus(ctx, projectID, "running")
	return map[string]any{
		"deployment_id": deploymentID,
		"commit_before": commitBefore,
		"commit_after":  commitAfter,
		"duration_ms":   finished.Sub(started).Milliseconds(),
	}, nil
}

func (h *DeploymentHandler) detectRuntime(ctx context.Context, project Project, workDir string) (string, error) {
	bestName := ""
	bestConfidence := -1
	for _, name := range h.runtimes.List() {
		runtimeProvider, ok := h.runtimes.Get(name)
		if !ok {
			continue
		}
		detection, err := runtimeProvider.Detect(ctx, runtimeContext(project, workDir))
		if err != nil || !detection.Detected {
			continue
		}
		confidence := detectionConfidence(detection)
		if confidence > bestConfidence {
			bestName = name
			bestConfidence = confidence
		}
	}
	if bestName == "" {
		return "", errors.New("provider unavailable: no runtime detected")
	}
	return bestName, nil
}

func detectionConfidence(detection runtimes.Detection) int {
	if detection.Metadata == nil {
		return 0
	}
	value, ok := detection.Metadata["confidence"]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func healthcheckPort(value string) (int, bool) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return 0, false
	}
	text := parsed.Port()
	if text == "" {
		switch parsed.Scheme {
		case "http":
			return 80, true
		case "https":
			return 443, true
		default:
			return 0, false
		}
	}
	port, err := strconv.Atoi(text)
	return port, err == nil && port > 0 && port <= 65535
}

func routeHostname(p Project) string {
	if p.Domain != nil && strings.TrimSpace(*p.Domain) != "" {
		return strings.TrimSpace(*p.Domain)
	}
	return p.Slug + ".localhost"
}

func runtimeContext(p Project, workDir string, ports ...int) runtimes.ProjectContext {
	config := map[string]any{
		"build_command":   p.BuildCommand,
		"start_command":   p.StartCommand,
		"healthcheck":     p.Healthcheck,
		"auto_start":      p.AutoStart,
		"deployment_mode": p.DeploymentMode,
	}
	if len(ports) > 0 && ports[0] > 0 {
		config["port"] = ports[0]
	}
	return runtimes.ProjectContext{ProjectID: p.ID, ProjectName: p.Name, WorkDir: workDir, Config: config}
}

func payloadString(payload map[string]any, key string) (string, error) {
	value, ok := payload[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("job payload missing %s", key)
	}
	return value, nil
}
func payloadBool(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func validDeploymentTransition(from, to string) bool {
	next := map[string]string{DeploymentQueued: DeploymentPreparing, DeploymentPreparing: DeploymentUpdatingSource, DeploymentUpdatingSource: DeploymentDependencies, DeploymentDependencies: DeploymentBuilding, DeploymentBuilding: DeploymentStarting, DeploymentStarting: DeploymentHealthcheck, DeploymentHealthcheck: DeploymentSuccess}
	return next[from] == to
}

var _ jobpkg.Handler = (*GitJobHandler)(nil)
var _ jobpkg.Handler = (*DeploymentHandler)(nil)

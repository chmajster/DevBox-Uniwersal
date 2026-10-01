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
	ComposePortBinding(ctx context.Context, directory, projectName string) (providers.ComposePortBinding, error)
	ComposeUp(ctx context.Context, directory, projectName, service string, binding *providers.ComposePortBinding) error
	ComposeDown(ctx context.Context, directory, projectName string) error
	ComposeHealthy(ctx context.Context, directory, projectName string) error
	ComposeTargetPort(ctx context.Context, directory, projectName string) (int, error)
}

type ManagedContainerDeployer interface {
	Available(ctx context.Context) error
	ManagedImageExists(ctx context.Context, image string) (bool, error)
	BuildManaged(ctx context.Context, spec containerspec.DeploymentSpec) error
	ReplaceManaged(ctx context.Context, spec containerspec.DeploymentSpec) error
}

type SharedNetworkDeployer interface {
	EnsureNetwork(ctx context.Context, name string) error
	ConnectComposeProjectNetwork(ctx context.Context, directory, projectName, network string) error
}

type DeploymentIntegrations struct {
	Ports         providers.PortAllocator
	Routes        ProjectRouteManager
	Compose       ComposeDeployer
	Managed       ManagedContainerDeployer
	Database      providers.ProjectDatabaseResolver
	Environment   runtimes.EnvironmentResolver
	Networks      SharedNetworkDeployer
	SharedNetwork string
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
		legacyBinding          *providers.ComposePortBinding
		legacyAllocatedPort    int
		portPlan               *deploymentPortPlan
		restoreComposePorts    func() error
		restorePreviousCompose bool
		composeCommitted       bool
		composeStarted         bool
		composeDir             string
		composeName            string
		databaseRuntime        = providers.ProjectDatabaseRuntime{Connection: providers.DatabaseConnection{Mode: providers.DatabaseModeNone}}
		databaseCleanup        func() error
		projectEnvironment     = runtimes.ResolvedEnvironment{Plain: map[string]string{}, Sensitive: map[string]string{}}
	)
	defer func() {
		if runErr == nil {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupSafe := true
		if composeStarted && !composeCommitted && h.integrations.Compose != nil {
			if err := h.integrations.Compose.ComposeDown(cleanupCtx, composeDir, composeName); err != nil {
				cleanupSafe = false
				// Docker may still be serving these ports; retain the leases
				// rather than hand them to a different application.
				if portPlan != nil {
					portPlan.activated = true
				}
				_ = h.logger.Log(cleanupCtx, job.ID, "error", "deployment.ports.cleanup_failed", map[string]any{"error": err.Error()})
			}
		}
		if restoreComposePorts != nil && !composeCommitted && cleanupSafe {
			if err := restoreComposePorts(); err != nil {
				_ = h.logger.Log(cleanupCtx, job.ID, "error", "deployment.ports.restore_failed", map[string]any{"error": err.Error()})
			} else if composeStarted && restorePreviousCompose {
				if err := h.integrations.Compose.ComposeUp(cleanupCtx, composeDir, composeName, "", nil); err != nil {
					_ = h.logger.Log(cleanupCtx, job.ID, "error", "deployment.compose.restore_failed", map[string]any{"error": err.Error()})
				}
			}
		}
		if databaseCleanup != nil {
			if err := databaseCleanup(); err != nil {
				_ = h.logger.Log(cleanupCtx, job.ID, "error", "deployment.database.cleanup_failed", map[string]any{"error": err.Error()})
			}
			databaseCleanup = nil
		}
		portPlan.rollback()
		if legacyAllocatedPort > 0 && cleanupSafe && !composeCommitted {
			_ = h.integrations.Ports.Release(cleanupCtx, legacyAllocatedPort)
		}
		finished := time.Now().UTC()
		_ = h.repo.FinishDeployment(context.Background(), deploymentID, DeploymentFailed, currentStage, commitAfter, runErr.Error(), finished, finished.Sub(started))
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
	if config.ContainerPolicy == ContainerPolicyGeneratedCompose {
		if strings.TrimSpace(config.Runtime) == "" && strings.TrimSpace(p.Runtime) == "" {
			runtimeName, detectErr := h.detectRuntime(ctx, p, workDir)
			if detectErr != nil {
				return nil, detectErr
			}
			config.Runtime = runtimeName
			p.Runtime = runtimeName
			if err := h.repo.UpdateRuntime(ctx, p.ID, runtimeName); err != nil {
				return nil, err
			}
		}
		generated, err := generateDevBoxCompose(p, config, workDir, commitAfter)
		if err != nil {
			return nil, err
		}
		_ = h.logger.Log(ctx, job.ID, "info", "deployment.compose.generated", map[string]any{
			"compose_path": generated.ComposePath,
			"dockerfile_path": generated.DockerfilePath,
			"runtime": generated.Runtime,
		})
	}
	hasCompose := projectHasCompose(workDir)
	hasDockerfile := projectHasDockerfile(workDir)
	if config.ContainerPolicy == ContainerPolicyCustom && !hasCompose && !hasDockerfile {
		return nil, errors.New("custom container policy requires compose.yaml/docker-compose.yml or Dockerfile")
	}
	if config.ContainerPolicy == ContainerPolicyGeneratedCompose && !hasCompose {
		return nil, errors.New("generated Compose policy did not produce a compose.yaml file")
	}

	if err := setStage(DeploymentDatabase); err != nil {
		return nil, err
	}
	if h.integrations.Environment != nil {
		projectEnvironment, err = h.integrations.Environment.ResolveEnvironment(ctx, p.ID)
		if err != nil {
			return nil, err
		}
	}
	if h.integrations.Database != nil {
		databaseRuntime, err = h.integrations.Database.ResolveRuntimeDatabase(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		if len(databaseRuntime.Secret) > 0 {
			defer clear(databaseRuntime.Secret)
		}
		if databaseRuntimeConfigured(databaseRuntime) {
			_ = h.logger.Log(ctx, job.ID, "info", "deployment.database.resolved", databaseLogFields(databaseRuntime))
			if databaseRuntime.Connection.Mode != providers.DatabaseModeNone &&
				!databaseRuntime.HostAccessOnly && databaseRuntime.Connection.Mode != providers.DatabaseModeCompose {
				if err := h.integrations.Database.TestApplicationConnection(ctx, p.ID); err != nil {
					return nil, err
				}
				_ = h.logger.Log(ctx, job.ID, "info", "deployment.database.tested", map[string]any{"mode": databaseRuntime.Connection.Mode})
			}
		}
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
		if h.integrations.SharedNetwork != "" {
			if h.integrations.Networks == nil {
				return nil, errors.New("provider unavailable: shared Docker application network")
			}
			if err := h.integrations.Networks.EnsureNetwork(ctx, h.integrations.SharedNetwork); err != nil {
				return nil, fmt.Errorf("ensure shared Docker application network: %w", err)
			}
		}
		composeEnvironment := mergedComposeEnvironment(projectEnvironment, databaseRuntime)
		if len(composeEnvironment) > 0 || databaseRuntimeConfigured(databaseRuntime) {
			databaseProvider, ok := h.integrations.Compose.(providers.ComposeDatabaseProvider)
			if !ok {
				return nil, errors.New("provider unavailable: Compose environment/database integration")
			}
			if databaseRuntime.Connection.Mode == providers.DatabaseModeCompose {
				services, err := databaseProvider.InspectComposeServices(ctx, composeDir, composeName)
				if err != nil {
					return nil, err
				}
				if !containsString(services, databaseRuntime.DatabaseService) {
					return nil, fmt.Errorf("Compose database service does not exist: %s", databaseRuntime.DatabaseService)
				}
			}
			databaseCleanup, err = databaseProvider.ConfigureComposeDatabase(ctx, composeDir, composeName, providers.ComposeDatabaseConfig{
				ApplicationService: databaseRuntime.ApplicationService,
				Environment:        composeEnvironment,
				Network:            databaseRuntime.Network,
				HostGateway:        databaseRuntime.HostGateway,
			})
			if err != nil {
				return nil, err
			}
		}
		network, err := h.repo.PortConfiguration(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		proxyMode := reverseProxyMode(network.Settings)
		if proxyMode == "disabled" {
			if publisher, ok := h.integrations.Compose.(providers.ComposePortPublisher); ok {
				if err := publisher.ClearComposePorts(composeDir, composeName); err != nil {
					return nil, err
				}
			}
		} else if !network.Configured {
			if publisher, ok := h.integrations.Compose.(providers.ComposePortPublisher); ok {
				// Fresh automatic projects must not inherit a stale override
				// from an older project using the same source path/slug.
				if err := publisher.ClearComposePorts(composeDir, composeName); err != nil {
					return nil, err
				}
			}
		}
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
		if err := setStage(DeploymentRuntimeConfiguration); err != nil {
			return nil, err
		}

		waitForPortConfiguration := func(discovery providers.ComposePortDiscovery, stale bool) (map[string]any, error) {
			if databaseCleanup != nil {
				if cleanupErr := databaseCleanup(); cleanupErr != nil {
					_ = h.logger.Log(ctx, job.ID, "warn", "deployment.database.cleanup_warning", map[string]any{"warning": cleanupErr.Error()})
				}
				databaseCleanup = nil
			}
			message := composePortDiscoveryMessage(discovery, stale)
			if err := h.repo.SetDeploymentWaiting(ctx, deploymentID, DeploymentRuntimeConfiguration, message); err != nil {
				return nil, err
			}
			_ = h.repo.UpdateStatus(ctx, p.ID, "waiting_for_configuration")
			_ = h.logger.Log(ctx, job.ID, "warn", "deployment.compose.port.waiting_for_configuration", map[string]any{
				"project_id": p.ID, "candidates": discovery.Candidates, "infrastructure": discovery.Infrastructure,
			})
			return map[string]any{
				"deployment_id": deploymentID,
				"status": DeploymentWaitingForConfiguration,
				"candidates": discovery.Candidates,
			}, nil
		}

		allocateLegacyBinding := func(binding providers.ComposePortBinding, preferredHealthcheck string) error {
			assignedPort := 0
			if p.Port != nil && *p.Port > 0 {
				assignedPort = *p.Port
			} else {
				if h.integrations.Ports == nil {
					return errors.New("provider unavailable: port allocator")
				}
				startPort := binding.RequestedHostPort
				switch startPort {
				case 80:
					startPort = 8080
				case 443:
					startPort = 8443
				}
				if parsed, ok := healthcheckPort(preferredHealthcheck); ok {
					startPort = parsed
				}
				if startPort < 1 || startPort > 65535 {
					startPort = 8080
				}
				lease, reserveErr := h.integrations.Ports.ReserveFrom(ctx, p.ID, "application", startPort)
				if reserveErr != nil {
					return fmt.Errorf("allocate compose host port from %d: %w", startPort, reserveErr)
				}
				assignedPort = lease.Port
				legacyAllocatedPort = lease.Port
				_ = h.logger.Log(ctx, job.ID, "info", "deployment.compose.port.allocated", map[string]any{
					"project_id": p.ID, "requested_port": startPort, "assigned_port": assignedPort,
				})
			}
			binding.HostPort = assignedPort
			legacyBinding = &binding
			return nil
		}

		if proxyMode != "disabled" {
			manualSelection := network.Settings.ReverseProxyMode == "manual" ||
				network.Settings.DetectionMode == "manual" ||
				(network.Configured && network.Settings.DetectionMode == "" &&
					(network.Settings.ComposeService != "" || network.Settings.ContainerPort > 0))
			if manualSelection {
				publisher, ok := h.integrations.Compose.(providers.ComposePortPublisher)
				if !ok {
					return nil, fmt.Errorf("%w: configurable Compose port publishing", ErrProviderUnavailable)
				}
				target, targetErr := publisher.InspectComposePortTarget(ctx, composeDir, composeName, network.Settings.ComposeService)
				if targetErr != nil {
					if discoverer, ok := h.integrations.Compose.(providers.ComposePortDiscoverer); ok {
						discovery, discoveryErr := discoverer.DiscoverComposeApplicationPorts(ctx, composeDir, composeName, composeDiscoveryHealthcheck(p, network.Settings))
						if discoveryErr == nil {
							if _, saveErr := h.repo.saveComposePortDiscovery(ctx, p.ID, discovery); saveErr != nil {
								return nil, saveErr
							}
							return waitForPortConfiguration(discovery, true)
						}
					}
					return nil, targetErr
				}
				internalPort := network.Settings.ContainerPort
				if internalPort == 0 {
					internalPort = target.ContainerPort
				}
				if internalPort == 0 {
					if discoverer, ok := h.integrations.Compose.(providers.ComposePortDiscoverer); ok {
						discovery, discoveryErr := discoverer.DiscoverComposeApplicationPorts(ctx, composeDir, composeName, composeDiscoveryHealthcheck(p, network.Settings))
						if discoveryErr == nil {
							if _, saveErr := h.repo.saveComposePortDiscovery(ctx, p.ID, discovery); saveErr != nil {
								return nil, saveErr
							}
							return waitForPortConfiguration(discovery, true)
						}
					}
					return nil, errors.New("Nie znaleziono portu HTTP aplikacji.")
				}
				portPlan, err = h.preparePortPlan(ctx, p, network, internalPort)
				if err != nil {
					return nil, err
				}
				restorePreviousCompose = network.Applied != nil || p.Status == "running"
				restoreComposePorts, err = publisher.ConfigureComposePorts(ctx, composeDir, composeName, target.Service, portPlan.bindings())
				if err != nil {
					return nil, err
				}
				h.logPortPlan(ctx, job.ID, "reserved", portPlan)
			} else if discoverer, ok := h.integrations.Compose.(providers.ComposePortDiscoverer); ok {
				discovery, err := discoverer.DiscoverComposeApplicationPorts(ctx, composeDir, composeName, composeDiscoveryHealthcheck(p, network.Settings))
				if err != nil {
					return nil, fmt.Errorf("docker compose port discovery: %w", err)
				}
				if network.Settings.DetectionMode == "automatic" &&
					network.Settings.ComposeFingerprint != "" &&
					network.Settings.ComposeFingerprint == discovery.Fingerprint &&
					network.Settings.ComposeService != "" && network.Settings.ContainerPort > 0 {
					for _, candidate := range discovery.Candidates {
						if candidate.Service == network.Settings.ComposeService && candidate.ContainerPort == network.Settings.ContainerPort {
							cached := candidate
							discovery.Selected = &cached
							break
						}
					}
				}
				staleDiscovery := network.Settings.ComposeFingerprint != "" && network.Settings.ComposeFingerprint != discovery.Fingerprint
				network, err = h.repo.saveComposePortDiscovery(ctx, p.ID, discovery)
				if err != nil {
					return nil, err
				}
				if discovery.Selected == nil {
					return waitForPortConfiguration(discovery, staleDiscovery)
				}
				selected := *discovery.Selected
				if selected.HostPort == 0 || network.Configured {
					publisher, ok := h.integrations.Compose.(providers.ComposePortPublisher)
					if !ok {
						return nil, fmt.Errorf("%w: configurable Compose port publishing", ErrProviderUnavailable)
					}
					portPlan, err = h.preparePortPlan(ctx, p, network, selected.ContainerPort)
					if err != nil {
						return nil, err
					}
					restorePreviousCompose = network.Applied != nil || p.Status == "running"
					restoreComposePorts, err = publisher.ConfigureComposePorts(ctx, composeDir, composeName, selected.Service, portPlan.bindings())
					if err != nil {
						return nil, err
					}
					h.logPortPlan(ctx, job.ID, "reserved", portPlan)
				} else {
					binding := providers.ComposePortBinding{
						Service: selected.Service, RequestedHostPort: selected.HostPort,
						HostPort: selected.HostPort, ContainerPort: selected.ContainerPort, Protocol: "tcp",
					}
					if err := allocateLegacyBinding(binding, composeDiscoveryHealthcheck(p, network.Settings)); err != nil {
						return nil, err
					}
				}
			} else {
				// Compatibility path for third-party Compose providers. The
				// built-in provider always implements structured discovery.
				binding, err := h.integrations.Compose.ComposePortBinding(ctx, composeDir, composeName)
				if err != nil {
					return nil, fmt.Errorf("docker compose port discovery: %w", err)
				}
				if err := allocateLegacyBinding(binding, composeDiscoveryHealthcheck(p, network.Settings)); err != nil {
					return nil, err
				}
			}
		}
		if err := setStage(DeploymentStarting); err != nil {
			return nil, err
		}
		// Compose can partially start services before returning an error.
		composeStarted = true
		if err := h.integrations.Compose.ComposeUp(ctx, composeDir, composeName, "", legacyBinding); err != nil {
			return nil, fmt.Errorf("docker compose up: %w", err)
		}
		if h.integrations.SharedNetwork != "" {
			if h.integrations.Networks == nil {
				return nil, errors.New("provider unavailable: shared Docker application network")
			}
			if err := h.integrations.Networks.ConnectComposeProjectNetwork(ctx, composeDir, composeName, h.integrations.SharedNetwork); err != nil {
				return nil, fmt.Errorf("connect Compose project to shared application network: %w", err)
			}
		}
		if err := setStage(DeploymentHealthcheck); err != nil {
			return nil, err
		}
		if err := h.integrations.Compose.ComposeHealthy(ctx, composeDir, composeName); err != nil {
			return nil, fmt.Errorf("docker compose healthcheck: %w", err)
		}
		if databaseRuntime.Connection.Mode == providers.DatabaseModeCompose && h.integrations.Database != nil {
			if err := h.integrations.Database.TestApplicationConnection(ctx, p.ID); err != nil {
				return nil, err
			}
			_ = h.logger.Log(ctx, job.ID, "info", "deployment.database.tested", map[string]any{"mode": databaseRuntime.Connection.Mode})
		}

		targetPort := 0
		if p.Port != nil {
			targetPort = *p.Port
		} else if parsed, ok := healthcheckPort(p.Healthcheck); ok {
			targetPort = parsed
		}
		if legacyBinding != nil {
			targetPort = legacyBinding.HostPort
		}
		if portPlan != nil {
			publisher := h.integrations.Compose.(providers.ComposePortPublisher)
			if err := publisher.CheckPublishedHTTP(ctx, portPlan.state.HTTP.HostPort); err != nil {
				return nil, fmt.Errorf("configured Compose HTTP port: %w", err)
			}
			composeCommitted = true
			if err := portPlan.commit(); err != nil {
				return nil, err
			}
			targetPort = portPlan.state.HTTP.HostPort
			h.logPortPlan(ctx, job.ID, "published", portPlan)
		}
		composeCommitted = true
		if err := setStage(DeploymentReverseProxy); err != nil {
			return nil, err
		}
		var routeWarning string
		if proxyMode != "disabled" && h.integrations.Routes != nil {
			if targetPort == 0 {
				routeWarning = "reverse proxy not configured: healthy application has no host target port"
			} else if err := h.integrations.Routes.EnsureProjectRoute(ctx, p.ID, routeHostname(p), targetPort); err != nil {
				routeWarning = "reverse proxy: " + err.Error()
			}
		}
		if routeWarning != "" {
			_ = h.logger.Log(ctx, job.ID, "warn", "deployment.reverse_proxy.warning", map[string]any{
				"project_id": p.ID,
				"warning":    routeWarning,
			})
		}
		var databaseCleanupWarning string
		if databaseCleanup != nil {
			if err := databaseCleanup(); err != nil {
				databaseCleanupWarning = "database runtime override cleanup: " + err.Error()
				_ = h.logger.Log(ctx, job.ID, "warn", "deployment.database.cleanup_warning", map[string]any{"warning": databaseCleanupWarning})
			}
			databaseCleanup = nil
		}
		return h.finishSuccess(ctx, deploymentID, p.ID, commitBefore, commitAfter, started, setStage, routeWarning, databaseCleanupWarning)
	}

	if h.integrations.Managed == nil {
		return nil, errors.New("provider unavailable: managed docker runtime")
	}
	if err := h.integrations.Managed.Available(ctx); err != nil {
		return nil, fmt.Errorf("provider unavailable: docker: %w", err)
	}

	network, err := h.repo.PortConfiguration(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if network.Settings.HTTPSEnabled && !hasDockerfile {
		return nil, fmt.Errorf("HTTPS publishing requires a custom Dockerfile or Compose service with a TLS listener; generated runtime images provide HTTP only")
	}
	port := network.Settings.HostPort

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
		if databaseRuntimeConfigured(databaseRuntime) && runtimeName == "php" {
			if databaseRuntimeUsesEngine(databaseRuntime, "mysql") && !hasPHPMySQLDriver(config.Modules) {
				return nil, errors.New("project PHP runtime does not contain pdo_mysql or mysqli")
			}
			if databaseRuntimeUsesEngine(databaseRuntime, "postgresql") && !hasPHPPostgreSQLDriver(config.Modules) {
				return nil, errors.New("project PHP runtime does not contain pgsql or pdo_pgsql")
			}
		}
		spec, err = containerspec.GenerateManaged(p.ID, workDir, runtimeName, config.RuntimeVersion, modules, commitAfter, port)
		if err != nil {
			return nil, fmt.Errorf("managed runtime specification: %w", err)
		}
	}

	mergeProjectEnvironment(&spec, projectEnvironment)
	if h.integrations.SharedNetwork != "" {
		if h.integrations.Networks == nil {
			return nil, errors.New("provider unavailable: shared Docker application network")
		}
		if err := h.integrations.Networks.EnsureNetwork(ctx, h.integrations.SharedNetwork); err != nil {
			return nil, fmt.Errorf("ensure shared Docker application network: %w", err)
		}
		if !containsString(spec.Networks, h.integrations.SharedNetwork) {
			spec.Networks = append(spec.Networks, h.integrations.SharedNetwork)
		}
	}
	if databaseRuntimeConfigured(databaseRuntime) {
		if !databaseRuntime.HostAccessOnly {
			mergeDatabaseEnvironment(&spec, projectDatabaseEnvironment(databaseRuntime))
			if databaseRuntime.Network != "" && !containsString(spec.Networks, databaseRuntime.Network) {
				spec.Networks = append(spec.Networks, databaseRuntime.Network)
			}
		}
		if databaseRuntime.HostGateway {
			if spec.ExtraHosts == nil {
				spec.ExtraHosts = make(map[string]string)
			}
			spec.ExtraHosts["host.docker.internal"] = "host-gateway"
		}
	}

	spec, err = containerspec.WithContainerPort(spec, network.Settings.ContainerPort)
	if err != nil {
		return nil, err
	}
	portPlan, err = h.preparePortPlan(ctx, p, network, spec.ContainerPort)
	if err != nil {
		return nil, err
	}
	spec.HostPort = portPlan.state.HTTP.HostPort
	port = spec.HostPort
	h.logPortPlan(ctx, job.ID, "reserved", portPlan)

	if err := setStage(DeploymentDependencies); err != nil {
		return nil, err
	}
	state, err := h.repo.RuntimeContainerState(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	imageExists, err := h.integrations.Managed.ManagedImageExists(ctx, spec.Image)
	if err != nil {
		return nil, fmt.Errorf("inspect managed image %q: %w", spec.Image, err)
	}
	needsBuild := forceRebuild || state.Fingerprint != spec.Fingerprint || state.ImageTag != spec.Image || !imageExists

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
	if err := setStage(DeploymentRuntimeConfiguration); err != nil {
		return nil, err
	}

	if err := setStage(DeploymentStarting); err != nil {
		return nil, err
	}
	if err := h.replaceWithPortPlan(ctx, spec, portPlan); err != nil {
		return nil, fmt.Errorf("replace managed container: %w", err)
	}
	if err := portPlan.commit(); err != nil {
		return nil, err
	}
	h.logPortPlan(ctx, job.ID, "published", portPlan)
	if err := setStage(DeploymentHealthcheck); err != nil {
		return nil, err
	}
	if err := h.repo.SaveRuntimeContainerState(ctx, p.ID, RuntimeContainerState{
		ContainerName: spec.ContainerName,
		ImageTag:      spec.Image,
		Fingerprint:   spec.Fingerprint,
	}); err != nil {
		return nil, err
	}
	if err := setStage(DeploymentReverseProxy); err != nil {
		return nil, err
	}
	var routeWarning string
	if reverseProxyMode(network.Settings) != "disabled" && h.integrations.Routes != nil {
		if err := h.integrations.Routes.EnsureProjectRoute(ctx, p.ID, routeHostname(p), port); err != nil {
			routeWarning = "reverse proxy: " + err.Error()
			_ = h.logger.Log(ctx, job.ID, "warn", "deployment.reverse_proxy.warning", map[string]any{
				"project_id": p.ID,
				"warning":    routeWarning,
			})
		}
	}

	return h.finishSuccess(ctx, deploymentID, p.ID, commitBefore, commitAfter, started, setStage, routeWarning)
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

func (h *DeploymentHandler) finishSuccess(ctx context.Context, deploymentID, projectID, commitBefore, commitAfter string, started time.Time, setStage func(string) error, warnings ...string) (map[string]any, error) {
	if err := setStage(DeploymentSuccess); err != nil {
		return nil, err
	}
	finished := time.Now().UTC()
	if err := h.repo.FinishDeployment(ctx, deploymentID, DeploymentSuccess, DeploymentSuccess, commitAfter, "", finished, finished.Sub(started)); err != nil {
		return nil, err
	}
	_ = h.repo.UpdateStatus(ctx, projectID, "running")
	result := map[string]any{
		"deployment_id": deploymentID,
		"commit_before": commitBefore,
		"commit_after":  commitAfter,
		"duration_ms":   finished.Sub(started).Milliseconds(),
	}
	nonEmptyWarnings := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if strings.TrimSpace(warning) != "" {
			nonEmptyWarnings = append(nonEmptyWarnings, warning)
		}
	}
	if len(nonEmptyWarnings) > 0 {
		result["warnings"] = nonEmptyWarnings
	}
	return result, nil
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

func composeDiscoveryHealthcheck(project Project, settings PortSettings) string {
	configured := strings.TrimSpace(settings.Healthcheck)
	if configured != "" && configured != "/" {
		return configured
	}
	return strings.TrimSpace(project.Healthcheck)
}

func composePortDiscoveryMessage(discovery providers.ComposePortDiscovery, stale bool) string {
	var text strings.Builder
	if stale {
		text.WriteString("Zapisana konfiguracja portu jest nieaktualna.\n\n")
	}
	if len(discovery.Candidates) > 0 {
		text.WriteString("Nie udało się jednoznacznie określić portu aplikacji.")
		text.WriteString("\n\nWykryte porty aplikacyjne:")
		for _, candidate := range discovery.Candidates {
			fmt.Fprintf(&text, "\n- %s:%d (%s)", candidate.Service, candidate.ContainerPort, candidate.Protocol)
		}
	} else {
		text.WriteString("Nie znaleziono portu HTTP aplikacji.")
	}
	if len(discovery.Infrastructure) > 0 {
		text.WriteString("\n\nWykryte serwisy infrastrukturalne:")
		for _, candidate := range discovery.Infrastructure {
			fmt.Fprintf(&text, "\n- %s:%d", candidate.Service, candidate.ContainerPort)
		}
		text.WriteString("\n\nSerwisy infrastrukturalne nie są używane przez reverse proxy.")
	}
	return text.String()
}

func routeHostname(p Project) string {
	if p.Domain != nil && strings.TrimSpace(*p.Domain) != "" {
		return strings.TrimSpace(*p.Domain)
	}
	return p.Slug + ".localhost"
}

func runtimeContext(p Project, workDir string, ports ...int) runtimes.ProjectContext {
	config := map[string]any{
		"build_command": p.BuildCommand,
		"start_command": p.StartCommand,
		"healthcheck":   p.Healthcheck,
		"auto_start":    p.AutoStart,
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
	next := map[string]string{
		DeploymentQueued:               DeploymentPreparing,
		DeploymentPreparing:            DeploymentUpdatingSource,
		DeploymentUpdatingSource:       DeploymentDatabase,
		DeploymentDatabase:             DeploymentDependencies,
		DeploymentDependencies:         DeploymentBuilding,
		DeploymentBuilding:             DeploymentRuntimeConfiguration,
		DeploymentRuntimeConfiguration: DeploymentStarting,
		DeploymentStarting:             DeploymentHealthcheck,
		DeploymentHealthcheck:          DeploymentReverseProxy,
		DeploymentReverseProxy:         DeploymentSuccess,
	}
	return next[from] == to
}

func mergedComposeEnvironment(environment runtimes.ResolvedEnvironment, database providers.ProjectDatabaseRuntime) map[string]string {
	result := make(map[string]string, len(environment.Plain)+len(environment.Sensitive)+10)
	for key, value := range environment.Plain {
		result[key] = value
	}
	for key, value := range environment.Sensitive {
		result[key] = value
	}
	if databaseRuntimeConfigured(database) && !database.HostAccessOnly {
		for key, value := range projectDatabaseEnvironment(database) {
			result[key] = value
		}
	}
	return result
}

func mergeProjectEnvironment(spec *containerspec.DeploymentSpec, environment runtimes.ResolvedEnvironment) {
	if spec.Environment == nil {
		spec.Environment = map[string]string{}
	}
	if spec.SensitiveEnvironment == nil {
		spec.SensitiveEnvironment = map[string]string{}
	}
	for key, value := range environment.Plain {
		spec.Environment[key] = value
		delete(spec.SensitiveEnvironment, key)
	}
	for key, value := range environment.Sensitive {
		delete(spec.Environment, key)
		spec.SensitiveEnvironment[key] = value
	}
}

func mergeDatabaseEnvironment(spec *containerspec.DeploymentSpec, database map[string]string) {
	if spec.SensitiveEnvironment == nil {
		spec.SensitiveEnvironment = map[string]string{}
	}
	for key, value := range database {
		delete(spec.Environment, key)
		spec.SensitiveEnvironment[key] = value
	}
}

func databaseLogFields(runtime providers.ProjectDatabaseRuntime) map[string]any {
	connection := runtime.Connection
	return map[string]any{
		"mode":             connection.Mode,
		"host":             connection.Host,
		"port":             connection.Port,
		"database":         connection.Database,
		"username":         connection.Username,
		"shared_services":  runtime.SharedServices,
		"host_access_only": runtime.HostAccessOnly,
	}
}

func databaseRuntimeConfigured(runtime providers.ProjectDatabaseRuntime) bool {
	return runtime.Connection.Mode != providers.DatabaseModeNone || len(runtime.SharedServices) > 0
}

func databaseRuntimeUsesEngine(runtime providers.ProjectDatabaseRuntime, engine string) bool {
	target := strings.ToLower(strings.TrimSpace(engine))
	if target == "mariadb" {
		target = "mysql"
	}
	if target == "postgres" {
		target = "postgresql"
	}
	if runtime.Connection.Mode != providers.DatabaseModeNone {
		current := strings.ToLower(strings.TrimSpace(runtime.Connection.Engine))
		if current == "" {
			current = "mysql"
		}
		if current == "mariadb" {
			current = "mysql"
		}
		if current == "postgres" {
			current = "postgresql"
		}
		return current == target
	}
	for _, service := range runtime.SharedServices {
		current := strings.ToLower(strings.TrimSpace(service.Engine))
		if current == "mariadb" {
			current = "mysql"
		}
		if current == "postgres" {
			current = "postgresql"
		}
		if current == target {
			return true
		}
	}
	return false
}

func projectDatabaseEnvironment(runtime providers.ProjectDatabaseRuntime) map[string]string {
	connection := runtime.Connection
	if connection.Mode != providers.DatabaseModeNone {
		port := strconv.Itoa(connection.Port)
		password := string(runtime.Secret)
		return map[string]string{
			"DB_DRIVER":         "mysql",
			"DB_HOST":           connection.Host,
			"DB_PORT":           port,
			"DB_DATABASE":       connection.Database,
			"DB_USERNAME":       connection.Username,
			"DB_PASSWORD":       password,
			"DATABASE_HOST":     connection.Host,
			"DATABASE_PORT":     port,
			"DATABASE_NAME":     connection.Database,
			"DATABASE_USER":     connection.Username,
			"DATABASE_PASSWORD": password,
		}
	}

	result := map[string]string{}
	for _, service := range runtime.SharedServices {
		port := strconv.Itoa(service.Port)
		switch strings.ToLower(strings.TrimSpace(service.Engine)) {
		case "mysql", "mariadb":
			result["MYSQL_HOST"] = service.Host
			result["MYSQL_PORT"] = port
		case "postgresql", "postgres":
			result["PGHOST"] = service.Host
			result["PGPORT"] = port
			result["POSTGRES_HOST"] = service.Host
			result["POSTGRES_PORT"] = port
		}
	}
	if len(runtime.SharedServices) == 1 {
		service := runtime.SharedServices[0]
		port := strconv.Itoa(service.Port)
		driver := "mysql"
		if databaseRuntimeUsesEngine(runtime, "postgresql") {
			driver = "pgsql"
		}
		result["DB_DRIVER"] = driver
		result["DB_HOST"] = service.Host
		result["DB_PORT"] = port
		result["DATABASE_HOST"] = service.Host
		result["DATABASE_PORT"] = port
	}
	return result
}

func hasPHPMySQLDriver(modules []RuntimeModule) bool {
	for _, module := range modules {
		switch strings.ToLower(strings.TrimSpace(module.Name)) {
		case "pdo_mysql", "mysqli":
			return true
		}
	}
	return false
}

func hasPHPPostgreSQLDriver(modules []RuntimeModule) bool {
	for _, module := range modules {
		switch strings.ToLower(strings.TrimSpace(module.Name)) {
		case "pgsql", "pdo_pgsql":
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var _ jobpkg.Handler = (*GitJobHandler)(nil)
var _ jobpkg.Handler = (*DeploymentHandler)(nil)

package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

var (
	ErrInvalidInput        = errors.New("invalid project input")
	ErrProviderUnavailable = errors.New("provider unavailable")
)

type Service struct {
	repo                 *Repository
	git                  *GitClient
	jobRunner            jobs.JobRunner
	secretStore          secrets.SecretStore
	projectsRoot         string
	directoryBrowseRoots []string
}

func NewService(repo *Repository, git *GitClient, jobRunner jobs.JobRunner, secretStore secrets.SecretStore, projectsRoot string, directoryBrowseRoots ...string) *Service {
	return &Service{
		repo:                 repo,
		git:                  git,
		jobRunner:            jobRunner,
		secretStore:          secretStore,
		projectsRoot:         projectsRoot,
		directoryBrowseRoots: normalizeDirectoryBrowseRoots(projectsRoot, directoryBrowseRoots),
	}
}

func (s *Service) List(ctx context.Context, includeArchived bool) ([]Project, error) {
	return s.repo.List(ctx, includeArchived)
}
func (s *Service) Get(ctx context.Context, id string) (Project, error) { return s.repo.Get(ctx, id) }

func (s *Service) Create(ctx context.Context, input CreateInput, actor *string) (Project, *domain.Job, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 120 {
		return Project{}, nil, fmt.Errorf("%w: name is required and must be at most 120 characters", ErrInvalidInput)
	}
	slug := Slugify(input.Name)
	if slug == "" {
		return Project{}, nil, fmt.Errorf("%w: name cannot produce an empty slug", ErrInvalidInput)
	}
	input.Runtime = containerspec.NormalizeRuntime(input.Runtime)
	input.ContainerPolicy = strings.ToLower(strings.TrimSpace(input.ContainerPolicy))
	if input.ContainerPolicy == "" {
		input.ContainerPolicy = ContainerPolicyAuto
	}
	if input.ContainerPolicy != ContainerPolicyAuto && input.ContainerPolicy != ContainerPolicyGeneratedCompose && input.ContainerPolicy != ContainerPolicyCustom {
		return Project{}, nil, fmt.Errorf("%w: container_policy must be auto, generated_compose or custom", ErrInvalidInput)
	}
	if input.Runtime != "" {
		if err := containerspec.Validate(input.Runtime, strings.TrimSpace(input.RuntimeVersion), nil); err != nil {
			return Project{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
	}
	if err := validateCredentialKind(input.CredentialKind); err != nil {
		return Project{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if input.CredentialID != "" && (input.CredentialKind != "" || input.CredentialValue != "") {
		return Project{}, nil, fmt.Errorf("%w: credential_id cannot be combined with inline credentials", ErrInvalidInput)
	}
	if input.CredentialKind != "" && input.CredentialValue == "" {
		return Project{}, nil, fmt.Errorf("%w: credential_value is required when credential_kind is set", ErrInvalidInput)
	}
	if input.CredentialID != "" {
		kind, err := s.repo.CentralCredentialKind(ctx, input.CredentialID)
		if err != nil {
			return Project{}, nil, err
		}
		input.CredentialKind = kind
	}

	id := NewID()
	p := Project{ID: id, Name: input.Name, Slug: slug, Description: strings.TrimSpace(input.Description), SourceType: input.SourceType, RepositoryURL: strings.TrimSpace(input.RepositoryURL), Branch: strings.TrimSpace(input.Branch), Runtime: input.Runtime, RuntimeVersion: strings.TrimSpace(input.RuntimeVersion), ContainerPolicy: input.ContainerPolicy, WorkingDirectory: strings.TrimSpace(input.WorkingDirectory), BuildCommand: strings.TrimSpace(input.BuildCommand), StartCommand: strings.TrimSpace(input.StartCommand), Healthcheck: strings.TrimSpace(input.Healthcheck), AutoStart: input.AutoStart, CredentialKind: input.CredentialKind, CredentialID: strings.TrimSpace(input.CredentialID), CreatedBy: actor, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if p.SourceType == "" {
		p.SourceType = SourceEmpty
	}
	switch p.SourceType {
	case SourceGit:
		if err := ValidateRepositoryURL(p.RepositoryURL); err != nil {
			return Project{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		if p.Branch != "" {
			if err := ValidateBranch(p.Branch); err != nil {
				return Project{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
		}
		path, err := SafeProjectPath(s.projectsRoot, slug)
		if err != nil {
			return Project{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		if _, err := os.Stat(path); err == nil {
			path, err = SafeProjectPath(s.projectsRoot, slug+"-"+id[:8])
			if err != nil {
				return Project{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
			if _, statErr := os.Stat(path); statErr == nil {
				return Project{}, nil, fmt.Errorf("%w: destination path already exists", ErrInvalidInput)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return Project{}, nil, fmt.Errorf("%w: inspect destination: %v", ErrInvalidInput, statErr)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return Project{}, nil, fmt.Errorf("%w: inspect destination: %v", ErrInvalidInput, err)
		}
		p.LocalPath, p.Status = path, "queued"
	case SourceLocal:
		path, err := ValidateExistingDirectory(input.LocalPath)
		if err != nil {
			return Project{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		p.LocalPath, p.Status = path, "ready"
		if s.git.IsRepository(ctx, path) {
			state, err := s.git.State(ctx, path)
			if err != nil {
				return Project{}, nil, err
			}
			p.Branch, p.CurrentCommit, p.RepositoryURL = state.Branch, state.Commit, state.Remote
		}
	case SourceEmpty:
		path, err := SafeProjectPath(s.projectsRoot, slug)
		if err != nil {
			return Project{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		if err := os.MkdirAll(path, 0o750); err != nil {
			return Project{}, nil, fmt.Errorf("create project directory: %w", err)
		}
		p.LocalPath, p.Status, p.RepositoryURL, p.Branch = path, "ready", "", ""
	default:
		return Project{}, nil, fmt.Errorf("%w: source_type must be git, local or empty", ErrInvalidInput)
	}
	credentialName := ""
	if input.CredentialKind != "" && input.CredentialID == "" {
		if s.secretStore == nil {
			return Project{}, nil, fmt.Errorf("%w: secret store is not configured", ErrProviderUnavailable)
		}
		credentialName = "default"
		if err := s.secretStore.Put(ctx, "git/"+id, credentialName, []byte(input.CredentialValue)); err != nil {
			return Project{}, nil, fmt.Errorf("store Git credential: %w", err)
		}
	}
	if err := s.repo.ReleaseArchivedIdentity(ctx, input.Name, slug); err != nil {
		if credentialName != "" {
			_ = s.secretStore.Delete(ctx, "git/"+id, credentialName)
		}
		return Project{}, nil, err
	}
	if err := s.repo.Create(ctx, p, credentialName); err != nil {
		if credentialName != "" {
			_ = s.secretStore.Delete(ctx, "git/"+id, credentialName)
		}
		return Project{}, nil, err
	}
	if p.SourceType == SourceGit {
		job, err := s.jobRunner.Enqueue(ctx, jobs.Request{Type: JobClone, ProjectID: &p.ID, RequestedBy: actor, Payload: map[string]any{"project_id": p.ID}})
		if err != nil {
			_ = s.repo.UpdateStatus(ctx, p.ID, "failed")
			return p, nil, err
		}
		return p, &job, nil
	}
	return p, nil, nil
}

func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Project, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return Project{}, err
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len(name) > 120 {
			return Project{}, fmt.Errorf("%w: invalid name", ErrInvalidInput)
		}
		p.Name = name
		p.Slug = Slugify(name)
	}
	if input.Description != nil {
		p.Description = strings.TrimSpace(*input.Description)
	}
	if input.RepositoryURL != nil {
		if p.SourceType != SourceGit {
			return Project{}, fmt.Errorf("%w: repository URL only applies to Git sources", ErrInvalidInput)
		}
		value := strings.TrimSpace(*input.RepositoryURL)
		if err := ValidateRepositoryURL(value); err != nil {
			return Project{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		p.RepositoryURL = value
	}
	if input.Branch != nil {
		value := strings.TrimSpace(*input.Branch)
		if value != "" {
			if err := ValidateBranch(value); err != nil {
				return Project{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
		}
		p.Branch = value
	}
	if input.LocalPath != nil {
		value := strings.TrimSpace(*input.LocalPath)
		if p.SourceType != SourceLocal {
			if value != p.LocalPath {
				return Project{}, fmt.Errorf("%w: local_path can only be changed for local sources", ErrInvalidInput)
			}
		} else {
			path, err := ValidateExistingDirectory(value)
			if err != nil {
				return Project{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
			p.LocalPath = path
			p.RepositoryURL = ""
			p.Branch = ""
			p.CurrentCommit = ""
			if s.git != nil && s.git.IsRepository(ctx, path) {
				state, err := s.git.State(ctx, path)
				if err != nil {
					return Project{}, fmt.Errorf("read Git state for updated local path: %w", err)
				}
				p.RepositoryURL = state.Remote
				p.Branch = state.Branch
				p.CurrentCommit = state.Commit
			}
		}
	}
	if input.Runtime != nil {
		value := containerspec.NormalizeRuntime(*input.Runtime)
		if value != "" {
			if err := containerspec.Validate(value, "", nil); err != nil {
				return Project{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
		}
		if value != p.Runtime {
			p.Runtime = value
			p.RuntimeVersion = ""
		}
	}
	if input.WorkingDirectory != nil {
		p.WorkingDirectory = strings.TrimSpace(*input.WorkingDirectory)
	}
	if input.BuildCommand != nil {
		p.BuildCommand = strings.TrimSpace(*input.BuildCommand)
	}
	if input.StartCommand != nil {
		p.StartCommand = strings.TrimSpace(*input.StartCommand)
	}
	if input.Healthcheck != nil {
		p.Healthcheck = strings.TrimSpace(*input.Healthcheck)
	}
	if input.AutoStart != nil {
		p.AutoStart = *input.AutoStart
	}
	credentialName := ""
	if p.CredentialKind != "" && p.CredentialID == "" {
		credentialName = "default"
	}
	if input.ClearCredential {
		if p.CredentialID == "" && s.secretStore != nil {
			_ = s.secretStore.Delete(ctx, "git/"+p.ID, "default")
		}
		p.CredentialKind = ""
		p.CredentialID = ""
		credentialName = ""
	}
	if input.CredentialID != nil {
		centralID := strings.TrimSpace(*input.CredentialID)
		if centralID == "" {
			p.CredentialID = ""
			p.CredentialKind = ""
			credentialName = ""
		} else {
			kind, err := s.repo.CentralCredentialKind(ctx, centralID)
			if err != nil {
				return Project{}, err
			}
			if p.CredentialID == "" && p.CredentialKind != "" && s.secretStore != nil {
				_ = s.secretStore.Delete(ctx, "git/"+p.ID, "default")
			}
			p.CredentialID = centralID
			p.CredentialKind = kind
			credentialName = ""
		}
	}
	if input.CredentialID == nil && (input.CredentialKind != nil || input.CredentialValue != nil) {
		kind := p.CredentialKind
		if input.CredentialKind != nil {
			kind = strings.TrimSpace(*input.CredentialKind)
		}
		if err := validateCredentialKind(kind); err != nil {
			return Project{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
		}
		if kind == "" {
			return Project{}, fmt.Errorf("%w: credential_kind is required to update credentials", ErrInvalidInput)
		}
		if input.CredentialValue == nil || *input.CredentialValue == "" {
			return Project{}, fmt.Errorf("%w: credential_value is required to update credentials", ErrInvalidInput)
		}
		if s.secretStore == nil {
			return Project{}, fmt.Errorf("%w: secret store is not configured", ErrProviderUnavailable)
		}
		if err := s.secretStore.Put(ctx, "git/"+p.ID, "default", []byte(*input.CredentialValue)); err != nil {
			return Project{}, err
		}
		p.CredentialID = ""
		p.CredentialKind, credentialName = kind, "default"
	}
	p.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, p, credentialName); err != nil {
		return Project{}, err
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	if p.CredentialKind != "" && p.CredentialID == "" && s.secretStore != nil {
		_ = s.secretStore.Delete(ctx, "git/"+id, "default")
	}
	return nil
}
func (s *Service) Archive(ctx context.Context, id string) error {
	return s.repo.Archive(ctx, id, time.Now().UTC())
}
func (s *Service) GitState(ctx context.Context, id string) (GitState, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return GitState{}, err
	}
	state, err := s.git.State(ctx, p.LocalPath)
	if err != nil {
		return GitState{}, err
	}
	_ = s.repo.UpdateGitState(ctx, p.ID, state.Branch, state.Commit)
	return state, nil
}
func (s *Service) EnqueueGit(ctx context.Context, id, operation string, actor *string) (domain.Job, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if err := s.git.CheckRepository(ctx, p.LocalPath); err != nil {
		return domain.Job{}, fmt.Errorf("%w: Git repository is not available: %v", ErrProviderUnavailable, err)
	}
	jobType := ""
	switch operation {
	case "fetch":
		jobType = JobFetch
	case "pull":
		jobType = JobPull
	default:
		return domain.Job{}, fmt.Errorf("%w: unsupported Git operation", ErrInvalidInput)
	}
	return s.jobRunner.Enqueue(ctx, jobs.Request{Type: jobType, ProjectID: &p.ID, RequestedBy: actor, Payload: map[string]any{"project_id": p.ID}})
}
func (s *Service) EnqueueCheckout(ctx context.Context, id, branch string, actor *string) (domain.Job, error) {
	if err := ValidateBranch(branch); err != nil {
		return domain.Job{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if err := s.git.CheckRepository(ctx, p.LocalPath); err != nil {
		return domain.Job{}, fmt.Errorf("%w: Git repository is not available: %v", ErrProviderUnavailable, err)
	}
	return s.jobRunner.Enqueue(ctx, jobs.Request{Type: JobCheckout, ProjectID: &p.ID, RequestedBy: actor, Payload: map[string]any{"project_id": p.ID, "branch": branch}})
}
func (s *Service) RuntimeContainerConfig(ctx context.Context, id string) (RuntimeContainerConfig, error) {
	return s.repo.RuntimeContainerConfig(ctx, id)
}

func (s *Service) RuntimeModules(runtime string) ([]containerspec.ModuleOption, error) {
	items, err := containerspec.Catalog(runtime)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return items, nil
}

func (s *Service) UpdateRuntimeContainerConfig(ctx context.Context, id string, config RuntimeContainerConfig) (RuntimeContainerConfig, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return RuntimeContainerConfig{}, err
	}
	config.ProjectID = id
	config.Runtime = containerspec.NormalizeRuntime(config.Runtime)
	config.RuntimeVersion = strings.TrimSpace(config.RuntimeVersion)
	config.ContainerPolicy = strings.ToLower(strings.TrimSpace(config.ContainerPolicy))
	if config.ContainerPolicy == "" {
		config.ContainerPolicy = ContainerPolicyAuto
	}
	if config.ContainerPolicy != ContainerPolicyAuto && config.ContainerPolicy != ContainerPolicyGeneratedCompose && config.ContainerPolicy != ContainerPolicyCustom {
		return RuntimeContainerConfig{}, fmt.Errorf("%w: container_policy must be auto, generated_compose or custom", ErrInvalidInput)
	}
	modules := make([]containerspec.Module, 0, len(config.Modules))
	for _, module := range config.Modules {
		modules = append(modules, containerspec.Module{Name: module.Name, Version: module.Version})
	}
	if config.Runtime == "" {
		if config.RuntimeVersion != "" || len(modules) > 0 {
			return RuntimeContainerConfig{}, fmt.Errorf("%w: runtime must be selected before configuring a version or modules", ErrInvalidInput)
		}
	} else if err := containerspec.Validate(config.Runtime, config.RuntimeVersion, modules); err != nil {
		return RuntimeContainerConfig{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := s.repo.SaveRuntimeContainerConfig(ctx, id, config); err != nil {
		return RuntimeContainerConfig{}, err
	}
	return s.repo.RuntimeContainerConfig(ctx, id)
}

func (s *Service) GenerateRuntimeCompose(ctx context.Context, id string) (GeneratedComposeResult, error) {
	project, err := s.repo.Get(ctx, id)
	if err != nil {
		return GeneratedComposeResult{}, err
	}
	config, err := s.repo.RuntimeContainerConfig(ctx, id)
	if err != nil {
		return GeneratedComposeResult{}, err
	}
	workDir, err := SafeWorkingDirectory(project.LocalPath, project.WorkingDirectory)
	if err != nil {
		return GeneratedComposeResult{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	return generateDevBoxCompose(project, config, workDir, project.CurrentCommit)
}

func (s *Service) RebuildRuntime(ctx context.Context, id string, actor *string) (domain.Job, error) {
	return s.enqueueDeployment(ctx, id, actor, false, true)
}

func (s *Service) Deploy(ctx context.Context, id string, actor *string) (domain.Job, error) {
	return s.enqueueDeployment(ctx, id, actor, false, false)
}

func (s *Service) ReconcileAutoStart(ctx context.Context) ([]domain.Job, error) {
	projects, err := s.repo.List(ctx, false)
	if err != nil {
		return nil, err
	}
	enqueued := make([]domain.Job, 0)
	for _, project := range projects {
		if !project.AutoStart || project.ArchivedAt != nil {
			continue
		}
		active, err := s.repo.HasActiveDeploymentJob(ctx, project.ID)
		if err != nil {
			return enqueued, err
		}
		if active {
			continue
		}
		job, err := s.enqueueDeployment(ctx, project.ID, nil, true, false)
		if err != nil {
			return enqueued, fmt.Errorf("reconcile auto-start project %s: %w", project.ID, err)
		}
		enqueued = append(enqueued, job)
	}
	return enqueued, nil
}

func (s *Service) enqueueDeployment(ctx context.Context, id string, actor *string, reconcile, forceRebuild bool) (domain.Job, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if p.ArchivedAt != nil {
		return domain.Job{}, fmt.Errorf("%w: archived projects cannot be deployed", ErrInvalidInput)
	}
	d := Deployment{ID: NewID(), ProjectID: id, Status: DeploymentQueued, Stage: DeploymentQueued, TriggeredBy: actor, CreatedAt: time.Now().UTC()}
	if err := s.repo.CreateDeployment(ctx, d); err != nil {
		return domain.Job{}, err
	}
	payload := map[string]any{"project_id": id, "deployment_id": d.ID}
	if reconcile {
		payload["reconcile"] = true
	}
	if forceRebuild {
		payload["force_rebuild"] = true
	}
	job, err := s.jobRunner.Enqueue(ctx, jobs.Request{Type: JobDeploy, ProjectID: &id, RequestedBy: actor, Payload: payload})
	if err != nil {
		_ = s.repo.FinishDeployment(ctx, d.ID, DeploymentFailed, DeploymentFailed, "", err.Error(), time.Now().UTC(), 0)
		return domain.Job{}, err
	}
	if err := s.repo.BindDeploymentJob(ctx, d.ID, job.ID); err != nil {
		return domain.Job{}, err
	}
	return job, nil
}
func (s *Service) Deployments(ctx context.Context, id string) ([]Deployment, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.ListDeployments(ctx, id)
}

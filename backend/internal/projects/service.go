package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

var (
	ErrInvalidInput        = errors.New("invalid project input")
	ErrProviderUnavailable = errors.New("provider unavailable")
)

type runtimeDefaultsApplier interface {
	ApplyDefaults(ctx context.Context, projectID string) error
}

type Service struct {
	repo                 *Repository
	git                  *GitClient
	jobRunner            jobs.JobRunner
	secretStore          secrets.SecretStore
	projectsRoot         string
	directoryBrowseRoots []string
	sourceControl        providers.ProjectSourceIntegration
	runtimeDefaults      runtimeDefaultsApplier
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

func (s *Service) SetSourceControlIntegration(integration providers.ProjectSourceIntegration) {
	s.sourceControl = integration
}

func (s *Service) SetRuntimeDefaults(defaults runtimeDefaultsApplier) {
	s.runtimeDefaults = defaults
}

func (s *Service) List(ctx context.Context, includeArchived bool) ([]Project, error) {
	return s.repo.List(ctx, includeArchived)
}
func (s *Service) Get(ctx context.Context, id string) (Project, error) { return s.repo.Get(ctx, id) }

func (s *Service) Create(ctx context.Context, input CreateInput, actor *string) (Project, *domain.Job, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = projectNameFromSource(input.RepositoryPath, input.RepositoryURL)
	}
	if input.Name == "" || len(input.Name) > 120 {
		return Project{}, nil, fmt.Errorf("%w: name is required and must be at most 120 characters", ErrInvalidInput)
	}
	slug := Slugify(input.Name)
	if slug == "" {
		return Project{}, nil, fmt.Errorf("%w: name cannot produce an empty slug", ErrInvalidInput)
	}
	if input.DeploymentMode == "" {
		input.DeploymentMode = "native"
	}
	if input.DeploymentMode != "native" && input.DeploymentMode != "docker" {
		return Project{}, nil, fmt.Errorf("%w: deployment_mode must be native or docker", ErrInvalidInput)
	}
	var sourceResolution *providers.ProjectSourceResolution
	if strings.TrimSpace(input.IntegrationID) != "" {
		if input.SourceType == "" {
			input.SourceType = SourceGit
		}
		if input.SourceType != SourceGit {
			return Project{}, nil, fmt.Errorf("%w: integration source requires source_type=git", ErrInvalidInput)
		}
		if s.sourceControl == nil {
			return Project{}, nil, fmt.Errorf("%w: source-control integrations are unavailable", ErrProviderUnavailable)
		}
		resolution, err := s.sourceControl.ResolveProjectSource(ctx, strings.TrimSpace(input.IntegrationID), strings.TrimSpace(input.RepositoryPath), strings.TrimSpace(input.Branch))
		if err != nil {
			return Project{}, nil, err
		}
		sourceResolution = &resolution
		input.RepositoryURL = resolution.Repository.CloneURL
		input.Branch = resolution.Branch
		input.CredentialID = resolution.CredentialID
		input.CredentialKind = ""
		input.CredentialValue = ""
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
	p := Project{ID: id, Name: input.Name, Slug: slug, Description: strings.TrimSpace(input.Description), SourceType: input.SourceType, RepositoryURL: strings.TrimSpace(input.RepositoryURL), Branch: strings.TrimSpace(input.Branch), Runtime: strings.TrimSpace(input.Runtime), DeploymentMode: input.DeploymentMode, WorkingDirectory: strings.TrimSpace(input.WorkingDirectory), BuildCommand: strings.TrimSpace(input.BuildCommand), StartCommand: strings.TrimSpace(input.StartCommand), Healthcheck: strings.TrimSpace(input.Healthcheck), AutoStart: input.AutoStart, CredentialKind: input.CredentialKind, CredentialID: strings.TrimSpace(input.CredentialID), CreatedBy: actor, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
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
			return Project{}, nil, fmt.Errorf("%w: destination path already exists", ErrInvalidInput)
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
	if err := s.repo.Create(ctx, p, credentialName); err != nil {
		if credentialName != "" {
			_ = s.secretStore.Delete(ctx, "git/"+id, credentialName)
		}
		return Project{}, nil, err
	}
	if sourceResolution != nil {
		if err := s.sourceControl.LinkProjectSource(ctx, p.ID, *sourceResolution); err != nil {
			_ = s.repo.Delete(context.Background(), p.ID)
			return Project{}, nil, fmt.Errorf("link source-control repository: %w", err)
		}
	}
	if s.runtimeDefaults != nil {
		if err := s.runtimeDefaults.ApplyDefaults(ctx, p.ID); err != nil {
			_ = s.repo.Delete(context.Background(), p.ID)
			return Project{}, nil, fmt.Errorf("apply runtime defaults: %w", err)
		}
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
	if input.Runtime != nil {
		p.Runtime = strings.TrimSpace(*input.Runtime)
	}
	if input.DeploymentMode != nil {
		value := strings.TrimSpace(*input.DeploymentMode)
		if value != "native" && value != "docker" {
			return Project{}, fmt.Errorf("%w: deployment_mode must be native or docker", ErrInvalidInput)
		}
		p.DeploymentMode = value
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
	if !s.git.IsRepository(ctx, p.LocalPath) {
		return GitState{}, fmt.Errorf("%w: Git repository is not available", ErrProviderUnavailable)
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
	if !s.git.IsRepository(ctx, p.LocalPath) {
		return domain.Job{}, fmt.Errorf("%w: Git repository is not available", ErrProviderUnavailable)
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
	if !s.git.IsRepository(ctx, p.LocalPath) {
		return domain.Job{}, fmt.Errorf("%w: Git repository is not available", ErrProviderUnavailable)
	}
	return s.jobRunner.Enqueue(ctx, jobs.Request{Type: JobCheckout, ProjectID: &p.ID, RequestedBy: actor, Payload: map[string]any{"project_id": p.ID, "branch": branch}})
}
func (s *Service) Deploy(ctx context.Context, id string, actor *string) (domain.Job, error) {
	return s.enqueueDeployment(ctx, id, actor, false)
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
		job, err := s.enqueueDeployment(ctx, project.ID, nil, true)
		if err != nil {
			return enqueued, fmt.Errorf("reconcile auto-start project %s: %w", project.ID, err)
		}
		enqueued = append(enqueued, job)
	}
	return enqueued, nil
}

func (s *Service) enqueueDeployment(ctx context.Context, id string, actor *string, reconcile bool) (domain.Job, error) {
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

func projectNameFromSource(repositoryPath, repositoryURL string) string {
	value := strings.Trim(strings.TrimSpace(repositoryPath), "/")
	if value == "" {
		value = strings.Trim(strings.TrimSpace(repositoryURL), "/")
		if index := strings.IndexAny(value, "?#"); index >= 0 {
			value = value[:index]
		}
	}
	if index := strings.LastIndex(value, "/"); index >= 0 {
		value = value[index+1:]
	}
	value = strings.TrimSuffix(value, ".git")
	return strings.TrimSpace(value)
}

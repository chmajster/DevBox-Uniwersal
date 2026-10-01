package applications

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

const (
	JobDetect    = "application.detect"
	JobDeploy    = "application.deploy"
	JobStart     = "application.start"
	JobStop      = "application.stop"
	JobRestart   = "application.restart"
	JobRemove    = "application.remove"
	JobReconcile = "application.reconcile"
)

type GitSourceProvider interface {
	Clone(context.Context, providers.GitSource) error
	PullWithCredential(context.Context, string, *string) error
	Checkout(context.Context, string, string) error
	Revision(context.Context, string) (string, error)
	IsRepository(context.Context, string) bool
}

type ContainerLogProvider interface {
	Logs(context.Context, string, int, bool) (io.ReadCloser, error)
}

type CredentialResolver interface {
	SecretRef(context.Context, string) (kind, scope, name string, err error)
}

type Service struct {
	secretStore  secrets.SecretStore
	mutationMu   sync.Mutex
	repo         *Repository
	drivers      *DriverRegistry
	selector     *Selector
	jobs         jobs.JobRunner
	git          GitSourceProvider
	logs         ContainerLogProvider
	credentials  CredentialResolver
	projectsRoot string
	allowedRoots []string
}

func NewService(repo *Repository, drivers *DriverRegistry, runner jobs.JobRunner, git GitSourceProvider, logs ContainerLogProvider, credentials CredentialResolver, projectsRoot string, allowedRoots ...string) *Service {
	if drivers == nil {
		drivers = NewDriverRegistry()
	}
	roots := append([]string{projectsRoot}, allowedRoots...)
	return &Service{repo: repo, drivers: drivers, selector: NewSelector(drivers), jobs: runner, git: git, logs: logs, credentials: credentials, projectsRoot: projectsRoot, allowedRoots: normalizeRoots(roots)}
}

type Summary struct {
	ActiveOperation *ActiveOperation `json:"active_operation,omitempty"`
	Application
	Source          Source      `json:"source"`
	Runtime         *Runtime    `json:"runtime,omitempty"`
	WorkloadCount   int         `json:"workload_count"`
	PrimaryEndpoint *Endpoint   `json:"primary_endpoint,omitempty"`
	LastDeployment  *Deployment `json:"last_deployment,omitempty"`
	Status          string      `json:"status"`
}

type Detail struct {
	ActiveOperation *ActiveOperation `json:"active_operation,omitempty"`
	Application
	Source      Source       `json:"source"`
	Runtime     *Runtime     `json:"runtime,omitempty"`
	Workloads   []Workload   `json:"workloads"`
	Endpoints   []Endpoint   `json:"endpoints"`
	Deployments []Deployment `json:"deployments"`
	Status      string       `json:"status"`
}

type DetectResponse struct {
	Detection *DetectionResult `json:"detection,omitempty"`
	Job       *domain.Job      `json:"job,omitempty"`
}

type DeleteOptions struct {
	RemoveContainers      bool `json:"remove_containers"`
	RemoveGeneratedImages bool `json:"remove_generated_images"`
	RemoveVolumes         bool `json:"remove_volumes"`
	RemoveSource          bool `json:"remove_source"`
	DeleteConfiguration   bool `json:"delete_configuration"`
}

type ApplicationLog struct {
	Workload string `json:"workload"`
	Line     string `json:"line"`
}

func (s *Service) List(ctx context.Context) ([]Summary, error) {
	apps, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(apps))
	for _, app := range apps {
		summary, err := s.summary(ctx, app)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *Service) summary(ctx context.Context, app Application) (Summary, error) {
	source, err := s.repo.Source(ctx, app.ID)
	if err != nil {
		return Summary{}, err
	}
	runtime, err := s.repo.Runtime(ctx, app.ID)
	if err != nil {
		return Summary{}, err
	}
	workloads, err := s.repo.Workloads(ctx, app.ID)
	if err != nil {
		return Summary{}, err
	}
	endpoints, err := s.repo.Endpoints(ctx, app.ID)
	if err != nil {
		return Summary{}, err
	}
	deployments, err := s.repo.Deployments(ctx, app.ID)
	if err != nil {
		return Summary{}, err
	}
	var primary *Endpoint
	for i := range endpoints {
		if endpoints[i].Primary {
			copy := endpoints[i]
			primary = &copy
			break
		}
	}
	var last *Deployment
	if len(deployments) > 0 {
		copy := deployments[0]
		last = &copy
	}
	operation, err := s.repo.ActiveOperation(ctx, app.ID)
	if err != nil {
		return Summary{}, err
	}
	return Summary{ActiveOperation: operation, Application: app, Source: source, Runtime: runtime, WorkloadCount: len(workloads), PrimaryEndpoint: primary, LastDeployment: last, Status: AggregateStatus(app.DesiredState, workloads)}, nil
}

func (s *Service) Get(ctx context.Context, id string) (Detail, error) {
	app, err := s.repo.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	source, err := s.repo.Source(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	runtime, err := s.repo.Runtime(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	workloads, err := s.repo.Workloads(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	endpoints, err := s.repo.Endpoints(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	deployments, err := s.repo.Deployments(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	operation, err := s.repo.ActiveOperation(ctx, app.ID)
	if err != nil {
		return Detail{}, err
	}
	return Detail{ActiveOperation: operation, Application: app, Source: source, Runtime: runtime, Workloads: workloads, Endpoints: endpoints, Deployments: deployments, Status: AggregateStatus(app.DesiredState, workloads)}, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput, actor *string) (Detail, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Detail{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	sourceType := strings.ToLower(strings.TrimSpace(input.SourceType))
	switch sourceType {
	case SourceGit, SourceLocal, SourceDockerImage, SourceEmpty:
	default:
		return Detail{}, fmt.Errorf("%w: unsupported source_type", ErrInvalidInput)
	}
	if err := validateConfiguration(input.Configuration); err != nil {
		return Detail{}, err
	}
	if input.Driver != "" {
		if _, ok := s.drivers.Get(input.Driver); !ok {
			return Detail{}, fmt.Errorf("%w: unknown deployment driver", ErrInvalidInput)
		}
	}
	if parsed, err := url.Parse(input.Source.RepositoryURL); err == nil && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return Detail{}, fmt.Errorf("%w: use a credential reference, not a password in the Git URL", ErrInvalidInput)
		}
	}
	slug := slugify(name)
	if slug == "" {
		return Detail{}, fmt.Errorf("%w: name cannot produce a valid slug", ErrInvalidInput)
	}
	now := time.Now().UTC()
	id := NewID()
	desired := input.DesiredState
	if desired == "" {
		desired = DesiredStopped
	}
	if desired != DesiredRunning && desired != DesiredStopped {
		return Detail{}, fmt.Errorf("%w: desired_state", ErrInvalidInput)
	}
	source := Source{ApplicationID: id, RepositoryURL: strings.TrimSpace(input.Source.RepositoryURL), Reference: strings.TrimSpace(input.Source.Reference), LocalPath: strings.TrimSpace(input.Source.LocalPath), DockerImage: strings.TrimSpace(input.Source.DockerImage), CredentialID: input.Source.CredentialID}
	switch sourceType {
	case SourceGit:
		if source.RepositoryURL == "" {
			return Detail{}, fmt.Errorf("%w: repository_url is required", ErrInvalidInput)
		}
	case SourceLocal:
		if source.LocalPath == "" {
			return Detail{}, fmt.Errorf("%w: local_path is required", ErrInvalidInput)
		}
		path, err := s.validateLocalPath(source.LocalPath)
		if err != nil {
			return Detail{}, err
		}
		source.LocalPath = path
	case SourceDockerImage:
		if source.DockerImage == "" {
			return Detail{}, fmt.Errorf("%w: docker_image is required", ErrInvalidInput)
		}
	}
	configJSON, _ := json.Marshal(input.Configuration)
	app := Application{ID: id, Name: name, Slug: slug, Description: strings.TrimSpace(input.Description), SourceType: sourceType, SourceConfig: configJSON, Driver: strings.TrimSpace(input.Driver), DesiredState: desired, ObservedState: ObservedUnknown, HealthState: HealthUnknown, AutoStart: input.AutoStart, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Create(ctx, app, source); err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Detail, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if err := s.repo.CheckIdle(ctx, id); err != nil {
		return Detail{}, err
	}
	app, err := s.repo.Get(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return Detail{}, fmt.Errorf("%w: name", ErrInvalidInput)
		}
		app.Name = name
		// Slug is a stable resource/work-directory identity, not a display label.
	}
	if input.Description != nil {
		app.Description = strings.TrimSpace(*input.Description)
	}
	if input.Driver != nil {
		driver := strings.TrimSpace(*input.Driver)
		if driver != "" {
			if _, ok := s.drivers.Get(driver); !ok {
				return Detail{}, fmt.Errorf("%w: driver %q", ErrInvalidInput, driver)
			}
		}
		if driver != app.Driver {
			workloads, err := s.repo.Workloads(ctx, id)
			if err != nil {
				return Detail{}, err
			}
			if len(workloads) > 0 {
				return Detail{}, fmt.Errorf("%w: deployment driver cannot change after provisioning", ErrConflict)
			}
		}
		app.Driver = driver
	}
	if input.DesiredState != nil {
		if *input.DesiredState != DesiredRunning && *input.DesiredState != DesiredStopped {
			return Detail{}, fmt.Errorf("%w: desired_state", ErrInvalidInput)
		}
		app.DesiredState = *input.DesiredState
	}
	if input.Configuration != nil {
		if err := validateConfiguration(*input.Configuration); err != nil {
			return Detail{}, err
		}
		raw, err := json.Marshal(*input.Configuration)
		if err != nil {
			return Detail{}, fmt.Errorf("%w: configuration", ErrInvalidInput)
		}
		app.SourceConfig = raw
	}
	if input.AutoStart != nil {
		app.AutoStart = *input.AutoStart
	}
	app.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, app); err != nil {
		return Detail{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Detect(ctx context.Context, input CreateInput, actor *string) (DetectResponse, error) {
	if parsed, err := url.Parse(input.Source.RepositoryURL); err == nil && parsed.User != nil {
		if _, exists := parsed.User.Password(); exists {
			return DetectResponse{}, fmt.Errorf("%w: use saved Git credentials", ErrInvalidInput)
		}
	}

	input.SourceType = strings.ToLower(strings.TrimSpace(input.SourceType))
	if input.SourceType != SourceGit && input.SourceType != SourceLocal && input.SourceType != SourceEmpty && input.SourceType != SourceDockerImage {
		return DetectResponse{}, fmt.Errorf("%w: source_type", ErrInvalidInput)
	}
	if err := validateConfiguration(input.Configuration); err != nil {
		return DetectResponse{}, err
	}
	source := Source{RepositoryURL: strings.TrimSpace(input.Source.RepositoryURL), Reference: strings.TrimSpace(input.Source.Reference), LocalPath: strings.TrimSpace(input.Source.LocalPath), DockerImage: strings.TrimSpace(input.Source.DockerImage), CredentialID: input.Source.CredentialID}
	if input.SourceType == SourceGit {
		if source.RepositoryURL == "" {
			return DetectResponse{}, fmt.Errorf("%w: repository_url is required", ErrInvalidInput)
		}
		if s.jobs == nil {
			return DetectResponse{}, ErrProviderUnavailable
		}
		payload, err := inputPayload(input)
		if err != nil {
			return DetectResponse{}, err
		}
		job, err := s.jobs.Enqueue(ctx, jobs.Request{Type: JobDetect, RequestedBy: actor, Payload: payload})
		if err != nil {
			return DetectResponse{}, err
		}
		return DetectResponse{Job: &job}, nil
	}
	workDir := ""
	if input.SourceType == SourceLocal {
		var err error
		workDir, err = s.validateLocalPath(source.LocalPath)
		if err != nil {
			return DetectResponse{}, err
		}
	}
	result, err := s.selector.Detect(ctx, DetectRequest{SourceType: input.SourceType, Source: source, WorkDir: workDir, Configuration: input.Configuration}, input.Driver)
	if err != nil && !errors.Is(err, ErrConfigurationRequired) {
		return DetectResponse{}, err
	}
	return DetectResponse{Detection: &result}, nil
}

func (s *Service) EnqueueDeploy(ctx context.Context, id string, actor *string) (Deployment, domain.Job, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.jobs == nil {
		return Deployment{}, domain.Job{}, ErrProviderUnavailable
	}
	if err := s.repo.CheckIdle(ctx, id); err != nil {
		return Deployment{}, domain.Job{}, err
	}
	app, err := s.repo.Get(ctx, id)
	if err != nil {
		return Deployment{}, domain.Job{}, err
	}
	deployment := Deployment{ID: NewID(), ApplicationID: id, Driver: app.Driver, Status: "queued", Stage: StageQueued, TriggeredBy: actor, CreatedAt: time.Now().UTC()}
	if err := s.repo.CreateDeployment(ctx, deployment); err != nil {
		return Deployment{}, domain.Job{}, err
	}
	job, err := s.jobs.Enqueue(ctx, jobs.Request{Type: JobDeploy, ApplicationID: &id, RequestedBy: actor, Payload: map[string]any{"application_id": id, "deployment_id": deployment.ID}})
	if err != nil {
		_ = s.repo.FinishDeployment(context.Background(), deployment.ID, "failed", StageFailed, err.Error())
		return Deployment{}, domain.Job{}, err
	}
	if err := s.repo.BindDeploymentJob(ctx, deployment.ID, job.ID); err != nil {
		return Deployment{}, domain.Job{}, err
	}
	deployment.JobID = &job.ID
	return deployment, job, nil
}

func (s *Service) EnqueueLifecycle(ctx context.Context, id, action string, actor *string, payload map[string]any) (domain.Job, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if s.jobs == nil {
		return domain.Job{}, ErrProviderUnavailable
	}
	if err := s.repo.CheckIdle(ctx, id); err != nil {
		return domain.Job{}, err
	}
	if _, err := s.repo.Get(ctx, id); err != nil {
		return domain.Job{}, err
	}
	jobType := ""
	switch action {
	case "start":
		jobType = JobStart
	case "stop":
		jobType = JobStop
	case "restart":
		jobType = JobRestart
	case "reconcile":
		jobType = JobReconcile
	case "remove":
		jobType = JobRemove
	default:
		return domain.Job{}, fmt.Errorf("%w: unsupported action", ErrInvalidInput)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["application_id"] = id
	return s.jobs.Enqueue(ctx, jobs.Request{Type: jobType, ApplicationID: &id, RequestedBy: actor, Payload: payload})
}

func (s *Service) State(ctx context.Context, id string) (ApplicationState, error) {
	return s.reconcile(ctx, id)
}

func (s *Service) Workloads(ctx context.Context, id string) ([]Workload, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.Workloads(ctx, id)
}
func (s *Service) Endpoints(ctx context.Context, id string) ([]Endpoint, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.Endpoints(ctx, id)
}
func (s *Service) Deployments(ctx context.Context, id string) ([]Deployment, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.Deployments(ctx, id)
}
func (s *Service) Events(ctx context.Context, id string) ([]map[string]any, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.Events(ctx, id, 300)
}

func (s *Service) Logs(ctx context.Context, id string, tail int, workloadFilter string) ([]ApplicationLog, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	if s.logs == nil {
		return nil, ErrProviderUnavailable
	}
	workloads, err := s.repo.Workloads(ctx, id)
	if err != nil {
		return nil, err
	}
	if tail < 1 {
		tail = 200
	}
	if tail > 2000 {
		tail = 2000
	}
	secretValues, err := s.runtimeSecrets(ctx, id)
	if err != nil {
		return nil, err
	}
	out := []ApplicationLog{}
	for _, workload := range workloads {
		if workloadFilter != "" && workload.Name != workloadFilter {
			continue
		}
		if workload.DriverResourceID == "" {
			continue
		}
		reader, err := s.logs.Logs(ctx, workload.DriverResourceID, tail, false)
		if err != nil {
			if driverMissing(err) {
				continue
			}
			return nil, err
		}
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			out = append(out, ApplicationLog{Workload: workload.Name, Line: redactLogLine(redactSecretValues(scanner.Text(), secretValues))})
		}
		_ = reader.Close()
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) workDir(app Application, source Source) string {
	switch app.SourceType {
	case SourceLocal:
		return source.LocalPath
	case SourceGit, SourceEmpty:
		return filepath.Join(s.projectsRoot, app.Slug)
	default:
		return ""
	}
}

func (s *Service) prepareSource(ctx context.Context, app Application, source Source) (string, string, error) {
	workDir := s.workDir(app, source)
	switch app.SourceType {
	case SourceDockerImage:
		return "", "", nil
	case SourceLocal:
		resolved, err := s.validateLocalPath(workDir)
		if err != nil {
			return "", "", err
		}
		return resolved, s.revision(ctx, resolved), nil
	case SourceEmpty:
		if err := os.MkdirAll(workDir, 0o750); err != nil {
			return "", "", err
		}
		return workDir, "", nil
	case SourceGit:
		if s.git == nil {
			return "", "", fmt.Errorf("%w: git", ErrProviderUnavailable)
		}
		credentialRef, err := s.gitCredentialRef(ctx, source.CredentialID)
		if err != nil {
			return "", "", err
		}
		if s.git.IsRepository(ctx, workDir) {
			if err := s.git.PullWithCredential(ctx, workDir, credentialRef); err != nil {
				return "", "", err
			}
			if source.Reference != "" {
				if err := s.git.Checkout(ctx, workDir, source.Reference); err != nil {
					return "", "", err
				}
			}
		} else {
			if info, err := os.Stat(workDir); err == nil && info.IsDir() {
				entries, _ := os.ReadDir(workDir)
				if len(entries) > 0 {
					return "", "", fmt.Errorf("%w: Git destination is not empty", ErrConflict)
				}
			}
			if err := os.MkdirAll(filepath.Dir(workDir), 0o750); err != nil {
				return "", "", err
			}
			if err := s.git.Clone(ctx, providers.GitSource{RepositoryURL: source.RepositoryURL, Reference: source.Reference, Destination: workDir, CredentialRef: credentialRef}); err != nil {
				return "", "", err
			}
		}
		revision, err := s.git.Revision(ctx, workDir)
		if err != nil {
			return "", "", err
		}
		return workDir, revision, nil
	default:
		return "", "", fmt.Errorf("%w: source_type", ErrInvalidInput)
	}
}

func (s *Service) revision(ctx context.Context, workDir string) string {
	if s.git != nil && s.git.IsRepository(ctx, workDir) {
		value, _ := s.git.Revision(ctx, workDir)
		return value
	}
	return ""
}

func (s *Service) validateLocalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%w: local path is required", ErrInvalidInput)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("%w: local path is unavailable: %v", ErrInvalidInput, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: local path must be a directory", ErrInvalidInput)
	}
	for _, root := range s.allowedRoots {
		if pathWithin(resolved, root) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("%w: local path is outside allowed roots", ErrInvalidInput)
}

func normalizeRoots(roots []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
			absolute = resolved
		}
		absolute = filepath.Clean(absolute)
		if !seen[absolute] {
			seen[absolute] = true
			out = append(out, absolute)
		}
	}
	return out
}
func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}
func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

var secretKeyPattern = regexp.MustCompile(`(?i)(password|passwd|secret|token|private.?key|api.?key|credential)`)

func inputPayload(input CreateInput) (map[string]any, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(raw, &out)
	return out, err
}
func decodeCreateInput(payload map[string]any) (CreateInput, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return CreateInput{}, err
	}
	var out CreateInput
	err = json.Unmarshal(raw, &out)
	return out, err
}
func decodeConfiguration(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out
}
func driverMissing(err error) bool {
	return err != nil && (strings.Contains(strings.ToLower(err.Error()), "not found") || strings.Contains(strings.ToLower(err.Error()), "no such container"))
}
func redactLogLine(line string) string {
	for _, needle := range []string{"password=", "passwd=", "token=", "secret=", "authorization:", "api_key="} {
		lower := strings.ToLower(line)
		if index := strings.Index(lower, needle); index >= 0 {
			return line[:index] + needle + "***"
		}
	}
	return line
}
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (s *Service) gitCredentialRef(ctx context.Context, id *string) (*string, error) {
	if id == nil || strings.TrimSpace(*id) == "" {
		return nil, nil
	}
	value := strings.TrimSpace(*id)
	if strings.Count(value, "|") == 2 {
		return &value, nil
	}
	if s.credentials == nil {
		return nil, fmt.Errorf("%w: credential resolver is not configured", ErrProviderUnavailable)
	}
	kind, scope, name, err := s.credentials.SecretRef(ctx, value)
	if err != nil {
		return nil, fmt.Errorf("resolve Git credential: %w", err)
	}
	ref := kind + "|" + scope + "|" + name
	return &ref, nil
}

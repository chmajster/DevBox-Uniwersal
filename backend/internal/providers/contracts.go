package providers

import (
	"context"
	"io"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

type GitSource struct {
	RepositoryURL string
	Reference     string
	Destination   string
	CredentialRef *string
}

type GitProvider interface {
	Clone(ctx context.Context, source GitSource) error
	Pull(ctx context.Context, workDir string) error
	Checkout(ctx context.Context, workDir, reference string) error
	Revision(ctx context.Context, workDir string) (string, error)
}

type SourceControlUser struct {
	ID       string
	Username string
	Name     string
	WebURL   string
	Scopes   []string
}

type SourceControlNamespace struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	WebURL string `json:"web_url,omitempty"`
}

type SourceControlRepository struct {
	ID            string `json:"id"`
	Owner         string `json:"owner"`
	Path          string `json:"path"`
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url,omitempty"`
	WebURL        string `json:"web_url"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
}

type SourceControlBranch struct {
	Name      string `json:"name"`
	CommitSHA string `json:"commit_sha,omitempty"`
	Default   bool   `json:"default,omitempty"`
	Protected bool   `json:"protected,omitempty"`
}

type SourceControlTag struct {
	Name      string `json:"name"`
	CommitSHA string `json:"commit_sha,omitempty"`
}

type SourceControlPullRequest struct {
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	State        string    `json:"state"`
	Author       string    `json:"author"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	UpdatedAt    time.Time `json:"updated_at"`
	WebURL       string    `json:"web_url"`
}

type SourceControlCIStatus struct {
	Available  bool   `json:"available"`
	Provider   string `json:"provider"`
	Status     string `json:"status,omitempty"`
	Name       string `json:"name,omitempty"`
	RunNumber  int64  `json:"run_number,omitempty"`
	CommitSHA  string `json:"commit_sha,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	WebURL     string `json:"web_url,omitempty"`
	Message    string `json:"message,omitempty"`
}

type SourceControlPage struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	NextPage   int `json:"next_page,omitempty"`
	TotalCount int `json:"total_count,omitempty"`
}

type ProjectSourceResolution struct {
	IntegrationID string
	Provider      string
	CredentialID  string
	Repository    SourceControlRepository
	Branch        string
}

type ProjectSourceIntegration interface {
	ResolveProjectSource(ctx context.Context, integrationID, repositoryPath, branch string) (ProjectSourceResolution, error)
	LinkProjectSource(ctx context.Context, projectID string, resolution ProjectSourceResolution) error
}

type SourceControlIntegrationProvider interface {
	TestConnection(ctx context.Context) (SourceControlUser, error)
	CurrentUser(ctx context.Context) (SourceControlUser, error)
	ListNamespaces(ctx context.Context, search string, page, perPage int) ([]SourceControlNamespace, SourceControlPage, error)
	ListRepositories(ctx context.Context, namespace, search string, page, perPage int) ([]SourceControlRepository, SourceControlPage, error)
	GetRepository(ctx context.Context, repository string) (SourceControlRepository, error)
	ListBranches(ctx context.Context, repository, search string, page, perPage int) ([]SourceControlBranch, SourceControlPage, error)
	ListTags(ctx context.Context, repository string, page, perPage int) ([]SourceControlTag, SourceControlPage, error)
	ListPullRequests(ctx context.Context, repository, state string, page, perPage int) ([]SourceControlPullRequest, SourceControlPage, error)
	GetCommitStatus(ctx context.Context, repository, commitSHA string) (SourceControlCIStatus, error)
	BuildWebURL(repository string, kind string, identifier string) (string, error)
}

type ProcessSpec struct {
	Name        string
	Command     string
	Args        []string
	WorkDir     string
	Environment map[string]string
}

type ProcessInfo struct {
	Name      string
	State     string
	PID       *int
	StartedAt *time.Time
}

type ProcessManager interface {
	Start(ctx context.Context, spec ProcessSpec) (ProcessInfo, error)
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Status(ctx context.Context, name string) (ProcessInfo, error)
	Logs(ctx context.Context, name string, tail int, follow bool) (io.ReadCloser, error)
}

type ContainerSpec struct {
	Name        string
	Image       string
	Command     []string
	Environment map[string]string
	Ports       map[int]int
	Volumes     map[string]string
}

type ContainerInfo struct {
	ID    string
	Name  string
	State string
}

type DockerProvider interface {
	Available(ctx context.Context) error
	PullImage(ctx context.Context, image string) error
	Create(ctx context.Context, spec ContainerSpec) (ContainerInfo, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	Inspect(ctx context.Context, id string) (ContainerInfo, error)
	Logs(ctx context.Context, id string, tail int, follow bool) (io.ReadCloser, error)
}

type DatabaseSpec struct {
	Engine   string
	Name     string
	Owner    string
	Charset  string
	Metadata map[string]string
}

type DatabaseProvider interface {
	Validate(ctx context.Context) error
	CreateDatabase(ctx context.Context, spec DatabaseSpec) error
	DeleteDatabase(ctx context.Context, name string) error
	CreateUser(ctx context.Context, username string, secretRef string) error
	Grant(ctx context.Context, database, username string, privileges []string) error
	Revoke(ctx context.Context, database, username string) error
	Health(ctx context.Context) error
}

type ProxyRoute struct {
	Domain   string
	Upstream string
	TLS      bool
	Metadata map[string]string
}

type ReverseProxyProvider interface {
	Validate(ctx context.Context) error
	Apply(ctx context.Context, route ProxyRoute) error
	Remove(ctx context.Context, domain string) error
	Reload(ctx context.Context) error
}

type PortLease struct {
	Port      int
	ProjectID string
	Purpose   string
}

type PortAllocator interface {
	Reserve(ctx context.Context, projectID, purpose string, preferred *int) (PortLease, error)
	Release(ctx context.Context, port int) error
	IsAvailable(ctx context.Context, port int) (bool, error)
}

type ServiceSpec struct {
	Name        string
	Description string
	Command     string
	Args        []string
	WorkDir     string
	Environment map[string]string
	AutoStart   bool
}

type SystemServiceProvider interface {
	Install(ctx context.Context, spec ServiceSpec) error
	Uninstall(ctx context.Context, name string) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Restart(ctx context.Context, name string) error
	Status(ctx context.Context, name string) (string, error)
}

// Stable aliases keep cross-agent imports centralized without duplicating contracts.
type SecretStore = secrets.SecretStore
type JobRunner = jobs.JobRunner

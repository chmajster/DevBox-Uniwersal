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

type ContainerPortBinding struct {
	HostIP        string
	HostPort      int
	ContainerPort int
}

type ContainerSpec struct {
	Name                 string
	Image                string
	Command              []string
	Environment          map[string]string
	SensitiveEnvironment map[string]string
	Ports                map[int]int
	PortBindings         []ContainerPortBinding
	Volumes              map[string]string
	Networks             []string
	RestartPolicy        string
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

type ComposePortBinding struct {
	Service           string
	RequestedHostPort int
	HostPort          int
	ContainerPort     int
	Protocol          string
}

type PortAllocator interface {
	Reserve(ctx context.Context, projectID, purpose string, preferred *int) (PortLease, error)
	ReserveFrom(ctx context.Context, projectID, purpose string, start int) (PortLease, error)
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

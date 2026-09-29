package providers

import "context"

type DatabaseMode string

const (
	DatabaseModeNone     DatabaseMode = "none"
	DatabaseModeManaged  DatabaseMode = "managed"
	DatabaseModeCompose  DatabaseMode = "compose"
	DatabaseModeExternal DatabaseMode = "external"
)

type DatabaseEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type DatabaseConnection struct {
	Engine    string       `json:"engine"`
	Host      string       `json:"host"`
	Port      int          `json:"port"`
	Database  string       `json:"database"`
	Username  string       `json:"username"`
	SecretRef string       `json:"-"`
	Mode      DatabaseMode `json:"mode"`
}

type ProjectDatabaseRuntime struct {
	Connection         DatabaseConnection
	Secret             []byte
	Network            string
	ApplicationService string
	DatabaseService    string
	HostGateway        bool
	HostAccessOnly     bool
}

type ProjectDatabaseResolver interface {
	ResolveApplicationConnection(ctx context.Context, projectID string) (DatabaseConnection, error)
	ResolveRuntimeDatabase(ctx context.Context, projectID string) (ProjectDatabaseRuntime, error)
	TestApplicationConnection(ctx context.Context, projectID string) error
}

type DockerInfrastructureProvider interface {
	DockerProvider
	EnsureNetwork(ctx context.Context, name string) error
	EnsureVolume(ctx context.Context, name string) error
	EnsureImage(ctx context.Context, image string) error
	EnsureContainer(ctx context.Context, spec ContainerSpec) (ContainerInfo, error)
	ConnectNetwork(ctx context.Context, container, network string) error
	Restart(ctx context.Context, id string) error
}

type ComposeDatabaseConfig struct {
	ApplicationService string
	Environment        map[string]string
	Network            string
	HostGateway        bool
}

type ContainerDatabaseTester interface {
	TestDatabaseConnection(ctx context.Context, network string, connection DatabaseConnection, password []byte) error
}

type ComposeDatabaseProvider interface {
	ConfigureComposeDatabase(ctx context.Context, directory, projectName string, config ComposeDatabaseConfig) (func() error, error)
	InspectComposeServices(ctx context.Context, directory, projectName string) ([]string, error)
	TestComposeDatabase(ctx context.Context, directory, projectName, service string, connection DatabaseConnection, password []byte) error
}

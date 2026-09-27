package docker

type DockerVersion struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

type DeploymentMode string

const (
	DeploymentModeDocker        DeploymentMode = "Docker"
	DeploymentModeDockerCompose DeploymentMode = "DockerCompose"
)

type ProjectDeploymentSupport struct {
	Dockerfile  bool             `json:"dockerfile"`
	ComposeFile string           `json:"compose_file,omitempty"`
	Modes       []DeploymentMode `json:"modes"`
}

type Status struct {
	Available         bool   `json:"available"`
	ClientVersion     string `json:"client_version,omitempty"`
	ServerVersion     string `json:"server_version,omitempty"`
	EngineName        string `json:"engine_name,omitempty"`
	OperatingSystem   string `json:"operating_system,omitempty"`
	OSType            string `json:"os_type,omitempty"`
	Architecture      string `json:"architecture,omitempty"`
	DockerRootDir     string `json:"docker_root_dir,omitempty"`
	CPUs              int    `json:"cpus,omitempty"`
	MemoryBytes       int64  `json:"memory_bytes,omitempty"`
	Containers        int    `json:"containers,omitempty"`
	ContainersRunning int    `json:"containers_running,omitempty"`
	ContainersStopped int    `json:"containers_stopped,omitempty"`
	Images            int    `json:"images,omitempty"`
	Error             string `json:"error,omitempty"`
}

type Container struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Image     string `json:"image"`
	State     string `json:"state"`
	Status    string `json:"status"`
	Ports     string `json:"ports,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type ContainerDetail struct {
	Container
	Running    bool              `json:"running"`
	StartedAt  string            `json:"started_at,omitempty"`
	FinishedAt string            `json:"finished_at,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

type Image struct {
	ID           string `json:"id"`
	Repository   string `json:"repository"`
	Tag          string `json:"tag"`
	Digest       string `json:"digest,omitempty"`
	Size         string `json:"size,omitempty"`
	CreatedSince string `json:"created_since,omitempty"`
}

type Volume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Scope      string `json:"scope,omitempty"`
	Mountpoint string `json:"mountpoint,omitempty"`
}

type Network struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Scope    string `json:"scope,omitempty"`
	Internal string `json:"internal,omitempty"`
	IPv6     string `json:"ipv6,omitempty"`
}

type ComposeProject struct {
	Name       string `json:"name"`
	ConfigFile string `json:"config_file"`
}

type ComposeProcess struct {
	Name    string `json:"name,omitempty"`
	Service string `json:"service,omitempty"`
	State   string `json:"state,omitempty"`
	Health  string `json:"health,omitempty"`
	Image   string `json:"image,omitempty"`
}

type ExecCommand string

const (
	ExecEnv              ExecCommand = "env"
	ExecIdentity         ExecCommand = "identity"
	ExecProcesses        ExecCommand = "processes"
	ExecSystem           ExecCommand = "system"
	ExecWorkingDirectory ExecCommand = "working-directory"
)

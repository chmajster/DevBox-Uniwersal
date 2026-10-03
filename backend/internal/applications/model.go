package applications

import (
	"encoding/json"
	"time"
)

const (
	SourceGit         = "git"
	SourceLocal       = "local"
	SourceDockerImage = "docker_image"
	SourceEmpty       = "empty"

	DesiredRunning = "running"
	DesiredStopped = "stopped"

	ObservedRunning  = "running"
	ObservedStarting = "starting"
	ObservedStopped  = "stopped"
	ObservedExited   = "exited"
	ObservedMissing  = "missing"
	ObservedFailed   = "failed"
	ObservedUnknown  = "unknown"

	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
	HealthDegraded  = "degraded"
	HealthUnknown   = "unknown"

	StageQueued                  = "QUEUED"
	StageSource                  = "SOURCE"
	StageDetect                  = "DETECT"
	StagePlan                    = "PLAN"
	StagePrepare                 = "PREPARE"
	StageDependencies            = "DEPENDENCIES"
	StageBuildOrPull             = "BUILD_OR_PULL"
	StageCreate                  = "CREATE"
	StageNetwork                 = "NETWORK"
	StageStorage                 = "STORAGE"
	StageConfigure               = "CONFIGURE"
	StageStart                   = "START"
	StageHealthcheck             = "HEALTHCHECK"
	StageDiscover                = "DISCOVER"
	StageRouting                 = "ROUTING"
	StageFinalize                = "FINALIZE"
	StageSuccess                 = "SUCCESS"
	StageWaitingForConfiguration = "WAITING_FOR_CONFIGURATION"
	StageFailed                  = "FAILED"
	StageCancelled               = "CANCELLED"
	StageRollingBack             = "ROLLING_BACK"
	StageRolledBack              = "ROLLED_BACK"
)

type Application struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Slug          string          `json:"slug"`
	Description   string          `json:"description"`
	SourceType    string          `json:"source_type"`
	SourceConfig  json.RawMessage `json:"source_config,omitempty"`
	Driver        string          `json:"driver"`
	DesiredState  string          `json:"desired_state"`
	ObservedState string          `json:"observed_state"`
	HealthState   string          `json:"health_state"`
	AutoStart     bool            `json:"auto_start"`
	CreatedBy     *string         `json:"created_by,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type Source struct {
	ApplicationID   string  `json:"application_id"`
	RepositoryURL   string  `json:"repository_url,omitempty"`
	Reference       string  `json:"reference,omitempty"`
	LocalPath       string  `json:"local_path,omitempty"`
	DockerImage     string  `json:"docker_image,omitempty"`
	CredentialID    *string `json:"credential_id,omitempty"`
	CurrentRevision string  `json:"current_revision,omitempty"`
}

type Runtime struct {
	ApplicationID string         `json:"application_id"`
	Name          string         `json:"name"`
	Version       string         `json:"version,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type PHPModuleInventory struct {
	Available bool     `json:"available"`
	Modules   []string `json:"modules"`
	Message   string   `json:"message,omitempty"`
}

type Workload struct {
	ID               string         `json:"id"`
	ApplicationID    string         `json:"application_id"`
	Name             string         `json:"name"`
	Role             string         `json:"role"`
	DriverResourceID string         `json:"driver_resource_id,omitempty"`
	Image            string         `json:"image,omitempty"`
	DesiredState     string         `json:"desired_state"`
	ObservedState    string         `json:"observed_state"`
	HealthState      string         `json:"health_state"`
	Primary          bool           `json:"primary"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type Endpoint struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	WorkloadID    string    `json:"workload_id"`
	Name          string    `json:"name"`
	Protocol      string    `json:"protocol"`
	ContainerPort int       `json:"container_port"`
	HostPort      *int      `json:"host_port,omitempty"`
	Domain        *string   `json:"domain,omitempty"`
	Public        bool      `json:"public"`
	Primary       bool      `json:"primary"`
	TLSMode       string    `json:"tls_mode"`
	HealthPath    string    `json:"health_path,omitempty"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Volume struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	WorkloadID    string `json:"workload_id,omitempty"`
	Type          string `json:"type"`
	Source        string `json:"source,omitempty"`
	Target        string `json:"target"`
	ReadOnly      bool   `json:"read_only"`
	Persistent    bool   `json:"persistent"`
}

type Deployment struct {
	ID             string          `json:"id"`
	ApplicationID  string          `json:"application_id"`
	JobID          *string         `json:"job_id,omitempty"`
	Driver         string          `json:"driver"`
	Status         string          `json:"status"`
	Stage          string          `json:"stage"`
	SourceRevision string          `json:"source_revision,omitempty"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
	TriggeredBy    *string         `json:"triggered_by,omitempty"`
	Error          string          `json:"error,omitempty"`
	PlanSnapshot   json.RawMessage `json:"plan_snapshot,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

type PlannedWorkload struct {
	Name        string            `json:"name"`
	Role        string            `json:"role"`
	Image       string            `json:"image,omitempty"`
	Primary     bool              `json:"primary"`
	Runtime     string            `json:"runtime,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

type PlannedEndpoint struct {
	Name          string `json:"name"`
	Workload      string `json:"workload"`
	Protocol      string `json:"protocol"`
	ContainerPort int    `json:"container_port"`
	HostPort      int    `json:"host_port,omitempty"`
	Domain        string `json:"domain,omitempty"`
	Public        bool   `json:"public"`
	Primary       bool   `json:"primary"`
	TLSMode       string `json:"tls_mode,omitempty"`
	HealthPath    string `json:"health_path,omitempty"`
}

type HealthCheckPlan struct {
	Workload string `json:"workload"`
	Type     string `json:"type"`
	Path     string `json:"path,omitempty"`
	Port     int    `json:"port,omitempty"`
}

type DeploymentPlan struct {
	Version        int               `json:"version"`
	ApplicationID  string            `json:"application_id"`
	Driver         string            `json:"driver"`
	Runtime        *Runtime          `json:"runtime,omitempty"`
	SourceRevision string            `json:"source_revision,omitempty"`
	Workloads      []PlannedWorkload `json:"workloads"`
	Endpoints      []PlannedEndpoint `json:"endpoints"`
	Volumes        []Volume          `json:"volumes,omitempty"`
	Networks       []string          `json:"networks,omitempty"`
	Healthchecks   []HealthCheckPlan `json:"healthchecks,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
}

type ServiceDetection struct {
	Name          string         `json:"name"`
	SuggestedRole string         `json:"suggested_role"`
	Primary       bool           `json:"primary"`
	Confidence    string         `json:"confidence"`
	Reason        string         `json:"reason,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type EndpointDetection struct {
	Service       string `json:"service"`
	Protocol      string `json:"protocol"`
	ContainerPort int    `json:"container_port"`
	Primary       bool   `json:"primary"`
	Confidence    string `json:"confidence"`
	Reason        string `json:"reason,omitempty"`
}

type DetectionResult struct {
	Driver                string              `json:"driver"`
	Confidence            string              `json:"confidence"`
	Runtime               string              `json:"runtime,omitempty"`
	Version               string              `json:"version,omitempty"`
	Services              []ServiceDetection  `json:"services,omitempty"`
	Endpoints             []EndpointDetection `json:"endpoints,omitempty"`
	Warnings              []string            `json:"warnings,omitempty"`
	Reasons               []string            `json:"reasons,omitempty"`
	RequiresConfiguration bool                `json:"requires_configuration"`
}

type ApplicationState struct {
	ApplicationID string     `json:"application_id"`
	DesiredState  string     `json:"desired_state"`
	ObservedState string     `json:"observed_state"`
	HealthState   string     `json:"health_state"`
	Status        string     `json:"status"`
	Workloads     []Workload `json:"workloads"`
	CheckedAt     time.Time  `json:"checked_at"`
}

type CreateInput struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	SourceType    string         `json:"source_type"`
	Source        SourceInput    `json:"source"`
	Driver        string         `json:"driver,omitempty"`
	DesiredState  string         `json:"desired_state,omitempty"`
	AutoStart     bool           `json:"auto_start"`
	Configuration map[string]any `json:"configuration,omitempty"`
}

type SourceInput struct {
	RepositoryURL string  `json:"repository_url,omitempty"`
	Reference     string  `json:"reference,omitempty"`
	LocalPath     string  `json:"local_path,omitempty"`
	DockerImage   string  `json:"docker_image,omitempty"`
	CredentialID  *string `json:"credential_id,omitempty"`
}

type UpdateInput struct {
	Configuration *map[string]any `json:"configuration,omitempty"`
	Name          *string         `json:"name,omitempty"`
	Description   *string         `json:"description,omitempty"`
	Driver        *string         `json:"driver,omitempty"`
	DesiredState  *string         `json:"desired_state,omitempty"`
	AutoStart     *bool           `json:"auto_start,omitempty"`
}

package projects

import "time"

const (
	SourceGit   = "git"
	SourceLocal = "local"
	SourceEmpty = "empty"

	ContainerPolicyAuto   = "auto"
	ContainerPolicyCustom = "custom"

	DeploymentQueued         = "QUEUED"
	DeploymentPreparing      = "PREPARING"
	DeploymentUpdatingSource = "UPDATING_SOURCE"
	DeploymentDatabase       = "DATABASE"
	DeploymentDependencies   = "DEPENDENCIES"
	DeploymentBuilding       = "BUILDING"
	DeploymentStarting       = "STARTING"
	DeploymentHealthcheck    = "HEALTHCHECK"
	DeploymentSuccess        = "SUCCESS"
	DeploymentFailed         = "FAILED"
)

type RuntimeModule struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type RuntimeContainerConfig struct {
	ProjectID       string          `json:"project_id"`
	Runtime         string          `json:"runtime"`
	RuntimeVersion  string          `json:"runtime_version"`
	ContainerPolicy string          `json:"container_policy"`
	Modules         []RuntimeModule `json:"modules"`
	ContainerName   string          `json:"container_name,omitempty"`
	ImageTag        string          `json:"image_tag,omitempty"`
	Fingerprint     string          `json:"build_fingerprint,omitempty"`
}

type RuntimeContainerState struct {
	ContainerName string
	ImageTag      string
	Fingerprint   string
}

type Project struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Slug             string     `json:"slug"`
	Description      string     `json:"description"`
	Status           string     `json:"status"`
	SourceType       string     `json:"source_type"`
	RepositoryURL    string     `json:"repository_url,omitempty"`
	Branch           string     `json:"branch,omitempty"`
	LocalPath        string     `json:"local_path"`
	Runtime          string     `json:"runtime"`
	RuntimeVersion   string     `json:"runtime_version"`
	ContainerPolicy  string     `json:"container_policy"`
	WorkingDirectory string     `json:"working_directory"`
	BuildCommand     string     `json:"build_command"`
	StartCommand     string     `json:"start_command"`
	Healthcheck      string     `json:"healthcheck"`
	AutoStart        bool       `json:"auto_start"`
	CredentialKind   string     `json:"credential_kind,omitempty"`
	CredentialID     string     `json:"credential_id,omitempty"`
	CurrentCommit    string     `json:"current_commit,omitempty"`
	Port             *int       `json:"port,omitempty"`
	Domain           *string    `json:"domain,omitempty"`
	CreatedBy        *string    `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ArchivedAt       *time.Time `json:"archived_at,omitempty"`
}

type CreateInput struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	SourceType       string `json:"source_type"`
	RepositoryURL    string `json:"repository_url"`
	Branch           string `json:"branch"`
	LocalPath        string `json:"local_path"`
	Runtime          string `json:"runtime"`
	RuntimeVersion   string `json:"runtime_version"`
	ContainerPolicy  string `json:"container_policy"`
	WorkingDirectory string `json:"working_directory"`
	BuildCommand     string `json:"build_command"`
	StartCommand     string `json:"start_command"`
	Healthcheck      string `json:"healthcheck"`
	AutoStart        bool   `json:"auto_start"`
	CredentialKind   string `json:"credential_kind"`
	CredentialValue  string `json:"credential_value"`
	CredentialID     string `json:"credential_id"`
}

type UpdateInput struct {
	Name             *string `json:"name"`
	Description      *string `json:"description"`
	RepositoryURL    *string `json:"repository_url"`
	Branch           *string `json:"branch"`
	WorkingDirectory *string `json:"working_directory"`
	BuildCommand     *string `json:"build_command"`
	StartCommand     *string `json:"start_command"`
	Healthcheck      *string `json:"healthcheck"`
	AutoStart        *bool   `json:"auto_start"`
	CredentialKind   *string `json:"credential_kind"`
	CredentialValue  *string `json:"credential_value"`
	CredentialID     *string `json:"credential_id"`
	ClearCredential  bool    `json:"clear_credential"`
}

type Deployment struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	JobID        *string    `json:"job_id,omitempty"`
	CommitBefore string     `json:"commit_before,omitempty"`
	CommitAfter  string     `json:"commit_after,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	DurationMS   int64      `json:"duration_ms"`
	Status       string     `json:"status"`
	Stage        string     `json:"stage"`
	Error        string     `json:"error,omitempty"`
	TriggeredBy  *string    `json:"triggered_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type GitCommit struct {
	Hash    string    `json:"hash"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`
}

type GitState struct {
	Branch   string      `json:"branch"`
	Commit   string      `json:"commit"`
	Remote   string      `json:"remote"`
	Ahead    int         `json:"ahead"`
	Behind   int         `json:"behind"`
	Dirty    bool        `json:"dirty"`
	Branches []string    `json:"branches"`
	History  []GitCommit `json:"history"`
}

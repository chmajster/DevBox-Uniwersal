package runtimes

import (
	"context"
	"io"
	"time"
)

type ResolvedEnvironment struct {
	Environment          map[string]string
	SensitiveEnvironment map[string]string
}

type EnvironmentResolver interface {
	ResolveEnvironment(ctx context.Context, projectID string) (ResolvedEnvironment, error)
}

type ProjectContext struct {
	ProjectID   string
	ProjectName string
	WorkDir     string
	Environment map[string]string
	Config      map[string]any
}

type Detection struct {
	Detected bool           `json:"detected"`
	Runtime  string         `json:"runtime"`
	Version  string         `json:"version,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Warnings []string `json:"warnings,omitempty"`
	Errors   []string `json:"errors,omitempty"`
}

type ProcessStatus struct {
	State     string     `json:"state"`
	PID       *int       `json:"pid,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	ExitCode  *int       `json:"exit_code,omitempty"`
}

type HealthResult struct {
	Healthy   bool          `json:"healthy"`
	Message   string        `json:"message,omitempty"`
	Latency   time.Duration `json:"latency,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

type LogOptions struct {
	Tail   int
	Follow bool
}

type Runtime interface {
	Name() string
	Detect(ctx context.Context, project ProjectContext) (Detection, error)
	Validate(ctx context.Context, project ProjectContext) (ValidationResult, error)
	InstallDependencies(ctx context.Context, project ProjectContext) error
	Build(ctx context.Context, project ProjectContext) error
	Start(ctx context.Context, project ProjectContext) error
	Stop(ctx context.Context, project ProjectContext) error
	Restart(ctx context.Context, project ProjectContext) error
	Status(ctx context.Context, project ProjectContext) (ProcessStatus, error)
	Logs(ctx context.Context, project ProjectContext, options LogOptions) (io.ReadCloser, error)
	HealthCheck(ctx context.Context, project ProjectContext) (HealthResult, error)
}

type Registry interface {
	Register(runtime Runtime) error
	Get(name string) (Runtime, bool)
	List() []string
}

package runtimes

import "context"

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
type Runtime interface {
	Name() string
	Detect(context.Context, ProjectContext) (Detection, error)
}
type Registry interface {
	Register(Runtime) error
	Get(string) (Runtime, bool)
	List() []string
}

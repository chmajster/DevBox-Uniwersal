package applications

import "errors"

var (
	ErrNotFound              = errors.New("application not found")
	ErrInvalidInput          = errors.New("invalid application input")
	ErrConflict              = errors.New("application conflict")
	ErrProviderUnavailable   = errors.New("provider unavailable")
	ErrConfigurationRequired = errors.New("application configuration required")
)

type OperationError struct {
	Stage     string `json:"stage,omitempty"`
	Driver    string `json:"driver,omitempty"`
	Workload  string `json:"workload,omitempty"`
	Operation string `json:"operation,omitempty"`
	Reason    string `json:"reason"`
	Action    string `json:"actionable_message,omitempty"`
	BuildLog  string `json:"-"`
	Cause     error  `json:"-"`
}

func (e *OperationError) Error() string {
	if e.Action != "" {
		return e.Reason + ": " + e.Action
	}
	return e.Reason
}

func (e *OperationError) Unwrap() error { return e.Cause }

func BuildLogFromError(err error) string {
	var output interface{ BuildLogOutput() string }
	if errors.As(err, &output) {
		return output.BuildLogOutput()
	}
	return ""
}

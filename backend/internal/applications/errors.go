package applications

import "errors"

var (
	ErrNotFound              = errors.New("application not found")
	ErrInvalidInput          = errors.New("invalid application input")
	ErrConflict              = errors.New("application conflict")
	ErrProviderUnavailable   = errors.New("provider unavailable")
	ErrConfigurationRequired = errors.New("application configuration required")
	ErrDirectoryAccess        = errors.New("directory access denied")
)

type OperationError struct {
	Stage     string `json:"stage,omitempty"`
	Driver    string `json:"driver,omitempty"`
	Workload  string `json:"workload,omitempty"`
	Operation string `json:"operation,omitempty"`
	Reason    string `json:"reason"`
	Action    string `json:"actionable_message,omitempty"`
}

func (e *OperationError) Error() string {
	if e.Action != "" {
		return e.Reason + ": " + e.Action
	}
	return e.Reason
}

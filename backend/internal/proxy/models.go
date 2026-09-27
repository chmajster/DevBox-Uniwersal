package proxy

import "time"

type PortRecord struct {
	ID              string     `json:"id"`
	ProjectID       *string    `json:"project_id,omitempty"`
	Application     *string    `json:"application,omitempty"`
	Port            int        `json:"port"`
	Purpose         string     `json:"purpose"`
	State           string     `json:"state"`
	SocketAvailable bool       `json:"socket_available"`
	CreatedAt       time.Time  `json:"created_at"`
	ReleasedAt      *time.Time `json:"released_at,omitempty"`
}

type HealthResult struct {
	Status         string    `json:"status"`
	ResponseTimeMS int64     `json:"response_time_ms"`
	Error          string    `json:"error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
	Type           string    `json:"type,omitempty"`
	Target         string    `json:"target,omitempty"`
	ProjectID      string    `json:"project_id,omitempty"`
}

type Domain struct {
	ID          string        `json:"id"`
	ProjectID   string        `json:"project_id"`
	Application string        `json:"application,omitempty"`
	Hostname    string        `json:"hostname"`
	TargetPort  int           `json:"target_port"`
	Target      string        `json:"target"`
	TLSEnabled  bool          `json:"tls_enabled"`
	Status      string        `json:"status"`
	Health      *HealthResult `json:"health,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type HostChange struct {
	Applied           bool   `json:"applied"`
	RequiresPrivilege bool   `json:"requires_privilege"`
	Path              string `json:"path"`
	Instruction       string `json:"instruction,omitempty"`
}

type DomainMutationResult struct {
	Domain Domain     `json:"domain"`
	Hosts  HostChange `json:"hosts"`
}

type ProxyStatus struct {
	Detected    bool   `json:"detected"`
	Version     string `json:"version,omitempty"`
	ConfigValid bool   `json:"config_valid"`
	Error       string `json:"error,omitempty"`
}

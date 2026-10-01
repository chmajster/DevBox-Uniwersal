package domain

import "time"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	default:
		return false
	}
}

func (r Role) Rank() int {
	switch r {
	case RoleAdmin:
		return 30
	case RoleOperator:
		return 20
	case RoleViewer:
		return 10
	default:
		return 0
	}
}

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type Job struct {
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	Status        string         `json:"status"`
	ProjectID     *string        `json:"project_id,omitempty"`
	ApplicationID *string        `json:"application_id,omitempty"`
	RequestedBy   *string        `json:"requested_by,omitempty"`
	Payload       map[string]any `json:"payload,omitempty"`
	Result        map[string]any `json:"result,omitempty"`
	Error         *string        `json:"error,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	StartedAt     *time.Time     `json:"started_at,omitempty"`
	FinishedAt    *time.Time     `json:"finished_at,omitempty"`
}

type AuditEvent struct {
	ID           string         `json:"id"`
	ActorUserID  *string        `json:"actor_user_id,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   *string        `json:"resource_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	RemoteAddr   *string        `json:"remote_addr,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

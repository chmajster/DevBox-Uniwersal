package databases

import (
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type DatabaseMode = providers.DatabaseMode

const (
	DatabaseModeNone     = providers.DatabaseModeNone
	DatabaseModeManaged  = providers.DatabaseModeManaged
	DatabaseModeCompose  = providers.DatabaseModeCompose
	DatabaseModeExternal = providers.DatabaseModeExternal
)

type DatabaseBinding struct {
	ID                 string       `json:"id,omitempty"`
	ProjectID          string       `json:"project_id"`
	Mode               DatabaseMode `json:"mode"`
	DatabaseID         *string      `json:"database_id,omitempty"`
	ApplicationService string       `json:"application_service,omitempty"`
	ComposeService     string       `json:"compose_service,omitempty"`
	Engine             string       `json:"engine,omitempty"`
	Host               string       `json:"host,omitempty"`
	Port               int          `json:"port,omitempty"`
	Database           string       `json:"database,omitempty"`
	Username           string       `json:"username,omitempty"`
	SecretRef          string       `json:"-"`
	HasSecret          bool         `json:"has_secret"`
	HostAccessOnly     bool         `json:"host_access_only,omitempty"`
	ApplicationHost    string       `json:"application_host,omitempty"`
	ApplicationPort    int          `json:"application_port,omitempty"`
	Status             string       `json:"status,omitempty"`
	CreatedAt          time.Time    `json:"created_at,omitempty"`
	UpdatedAt          time.Time    `json:"updated_at,omitempty"`
}

type DatabaseBindingInput struct {
	Mode               DatabaseMode `json:"mode"`
	ApplicationService string       `json:"application_service,omitempty"`
	ComposeService     string       `json:"compose_service,omitempty"`
	Engine             string       `json:"engine,omitempty"`
	Host               string       `json:"host,omitempty"`
	Port               int          `json:"port,omitempty"`
	Database           string       `json:"database,omitempty"`
	Username           string       `json:"username,omitempty"`
	Password           string       `json:"password,omitempty"`
	PasswordProvided   bool         `json:"password_provided,omitempty"`
	HostAccessOnly     bool         `json:"host_access_only,omitempty"`
}

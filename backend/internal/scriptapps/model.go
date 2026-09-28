package scriptapps

import "time"

const (
	JobInstall   = "script_app_install"
	JobUpdate    = "script_app_update"
	JobUninstall = "script_app_uninstall"
	JobStart     = "script_app_start"
	JobStop      = "script_app_stop"
	JobRestart   = "script_app_restart"
)

type App struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	InstallSource   string    `json:"install_source"`
	UpdateSource    string    `json:"update_source,omitempty"`
	UninstallSource string    `json:"uninstall_source,omitempty"`
	Interpreter     string    `json:"interpreter"`
	ChecksumSHA256  string    `json:"checksum_sha256,omitempty"`
	RunAsRoot       bool      `json:"run_as_root"`
	AllowInsecure   bool      `json:"allow_insecure"`
	Manager         string    `json:"manager"`
	ManagerTarget   string    `json:"manager_target,omitempty"`
	Status          string    `json:"status"`
	LastError       string    `json:"last_error,omitempty"`
	CreatedBy       *string   `json:"created_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	InstallSource   string `json:"install_source"`
	UpdateSource    string `json:"update_source"`
	UninstallSource string `json:"uninstall_source"`
	Interpreter     string `json:"interpreter"`
	ChecksumSHA256  string `json:"checksum_sha256"`
	RunAsRoot       bool   `json:"run_as_root"`
	AllowInsecure   bool   `json:"allow_insecure"`
	ServiceName     string `json:"service_name"`
	InstallNow      bool   `json:"install_now"`
}

type CreateResult struct {
	App App `json:"app"`
	Job any `json:"job,omitempty"`
}

type LogsResult struct {
	Manager string `json:"manager"`
	Target  string `json:"target,omitempty"`
	Logs    string `json:"logs"`
}

package databases

import "time"

type Database struct {
	ID              string    `json:"id"`
	ProjectID       *string   `json:"project_id,omitempty"`
	ApplicationName string    `json:"application_name,omitempty"`
	Provider        string    `json:"provider"`
	Engine          string    `json:"engine"`
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	Username        string    `json:"user,omitempty"`
	SizeBytes       *int64    `json:"size_bytes,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type DatabaseUser struct {
	ID         string    `json:"id"`
	DatabaseID string    `json:"database_id"`
	Username   string    `json:"username"`
	SecretRef  string    `json:"-"`
	Privileges []string  `json:"privileges"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Backup struct {
	ID          string     `json:"id"`
	DatabaseID  string     `json:"database_id"`
	FileName    string     `json:"file_name"`
	Status      string     `json:"status"`
	SizeBytes   int64      `json:"size_bytes"`
	Error       *string    `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ProjectRef struct {
	ID               string
	Name             string
	Slug             string
	LocalPath        string
	WorkingDirectory string
}

type MySQLStatus struct {
	Version         string `json:"version,omitempty"`
	Running         bool   `json:"running"`
	ConnectionState string `json:"connection_state"`
	AdminHost       string `json:"admin_host,omitempty"`
	AdminPort       int    `json:"admin_port,omitempty"`
	ApplicationHost string `json:"application_host,omitempty"`
	ApplicationPort int    `json:"application_port,omitempty"`
	Network         string `json:"network,omitempty"`
}

type ConnectionConfig struct {
	Engine   string `json:"engine"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ProvisionResult struct {
	Database   Database         `json:"database"`
	Credential ConnectionConfig `json:"credential"`
}

type PHPMyAdminStatus struct {
	HostDatabaseAccess    bool   `json:"host_database_access"`
	HostDatabaseReachable bool   `json:"host_database_reachable"`
	HostDatabaseHost      string `json:"host_database_host,omitempty"`
	HostDatabasePort      int    `json:"host_database_port,omitempty"`
	Installed             bool   `json:"installed"`
	Running               bool   `json:"running"`
	State                 string `json:"state"`
	URL                   string `json:"url"`
}

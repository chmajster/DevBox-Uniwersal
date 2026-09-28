package backups

import "time"

type Backup struct {
	ID          string     `json:"id"`
	FileName    string     `json:"file_name"`
	Status      string     `json:"status"`
	SizeBytes   int64      `json:"size_bytes"`
	SHA256      string     `json:"sha256,omitempty"`
	RequestedBy *string    `json:"requested_by,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type ProjectArtifact struct {
	ID      string   `json:"id"`
	WorkDir string   `json:"work_dir"`
	Files   []string `json:"files,omitempty"`
}

type Manifest struct {
	SchemaVersion     int               `json:"schema_version"`
	AppVersion        string            `json:"app_version"`
	CreatedAt         time.Time         `json:"created_at"`
	RequiresMasterKey bool              `json:"requires_master_key"`
	DatabaseFile      string            `json:"database_file"`
	Projects          []ProjectArtifact `json:"projects,omitempty"`
	SkippedProjects   []string          `json:"skipped_projects,omitempty"`
}

type PendingRestore struct {
	BackupID   string    `json:"backup_id"`
	StagingDir string    `json:"staging_dir"`
	SHA256     string    `json:"sha256"`
	StagedAt   time.Time `json:"staged_at"`
}

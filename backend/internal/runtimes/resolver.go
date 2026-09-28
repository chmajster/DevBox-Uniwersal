package runtimes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrProjectNotFound       = errors.New("project not found")
	ErrProjectWorkDirMissing = errors.New("project work directory is not configured")
	ErrRuntimeNotDetected    = errors.New("runtime not detected")
)

type SecretReference struct {
	Scope string `json:"scope,omitempty"`
	Name  string `json:"name"`
}

type RuntimeConfig struct {
	Runtime           string
	Environment       map[string]string
	SecretEnvironment map[string]SecretReference
	Raw               map[string]any
}

type ResolvedProject struct {
	Context ProjectContext
	Config  RuntimeConfig
}

type ProjectResolver interface {
	Resolve(ctx context.Context, projectID string) (ResolvedProject, error)
}

type SQLiteProjectResolver struct {
	db *sql.DB
}

func NewSQLiteProjectResolver(db *sql.DB) *SQLiteProjectResolver {
	return &SQLiteProjectResolver{db: db}
}

func (r *SQLiteProjectResolver) Resolve(ctx context.Context, projectID string) (ResolvedProject, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ResolvedProject{}, ErrProjectNotFound
	}

	var name string
	var workDir sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT name, work_dir FROM projects WHERE id=?`, projectID).Scan(&name, &workDir)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolvedProject{}, ErrProjectNotFound
	}
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("resolve project: %w", err)
	}
	if !workDir.Valid || strings.TrimSpace(workDir.String) == "" {
		return ResolvedProject{}, ErrProjectWorkDirMissing
	}

	config := RuntimeConfig{
		Environment:       make(map[string]string),
		SecretEnvironment: make(map[string]SecretReference),
		Raw:               make(map[string]any),
	}
	var runtimeName, configJSON string
	err = r.db.QueryRowContext(
		ctx,
		`SELECT runtime, config_json FROM project_runtime_configs WHERE project_id=? ORDER BY updated_at DESC LIMIT 1`,
		projectID,
	).Scan(&runtimeName, &configJSON)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return ResolvedProject{}, fmt.Errorf("resolve runtime configuration: %w", err)
	default:
		decoded, decodeErr := decodeRuntimeConfig(runtimeName, configJSON, projectID)
		if decodeErr != nil {
			return ResolvedProject{}, decodeErr
		}
		config = decoded
	}

	projectContext := ProjectContext{
		ProjectID:       projectID,
		ProjectName:     name,
		WorkDir:         workDir.String,
		Environment:     copyStringMap(config.Environment),
		Config:          cloneAnyMap(config.Raw),
		Executables:     make(map[string]string),
		RuntimeVersions: make(map[string]string),
	}
	if err := r.applyAssignments(ctx, projectID, &projectContext); err != nil {
		return ResolvedProject{}, err
	}
	return ResolvedProject{Context: projectContext, Config: config}, nil
}

func (r *SQLiteProjectResolver) applyAssignments(ctx context.Context, projectID string, project *ProjectContext) error {
	rows, err := r.db.QueryContext(ctx, "SELECT a.runtime_type,a.resolved_version,i.executable_path,i.metadata_json FROM project_runtime_assignments a JOIN runtime_installations i ON i.id=a.runtime_installation_id WHERE a.project_id=? AND i.status='installed'", projectID)
	if err != nil {
		return fmt.Errorf("resolve runtime assignments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var runtimeType, version, executablePath, metadataJSON string
		if err := rows.Scan(&runtimeType, &version, &executablePath, &metadataJSON); err != nil {
			return err
		}
		project.Executables[runtimeType] = executablePath
		project.RuntimeVersions[runtimeType] = version
		var metadata struct {
			Tools map[string]string `json:"tools"`
		}
		if json.Unmarshal([]byte(metadataJSON), &metadata) == nil {
			for key, value := range metadata.Tools {
				if strings.TrimSpace(value) != "" {
					project.Executables[key] = value
				}
			}
		}
	}
	return rows.Err()
}

func decodeRuntimeConfig(runtimeName, configJSON, projectID string) (RuntimeConfig, error) {
	raw := make(map[string]any)
	if strings.TrimSpace(configJSON) != "" {
		if err := json.Unmarshal([]byte(configJSON), &raw); err != nil {
			return RuntimeConfig{}, fmt.Errorf("parse runtime config for project %s: %w", projectID, err)
		}
	}
	config := RuntimeConfig{
		Runtime:           strings.ToLower(strings.TrimSpace(runtimeName)),
		Environment:       make(map[string]string),
		SecretEnvironment: make(map[string]SecretReference),
		Raw:               raw,
	}

	if values, ok := raw["environment"].(map[string]any); ok {
		for key, value := range values {
			if text, ok := value.(string); ok {
				config.Environment[key] = text
			}
		}
	}
	if values, ok := raw["secret_environment"].(map[string]any); ok {
		for key, value := range values {
			switch typed := value.(type) {
			case string:
				if strings.TrimSpace(typed) != "" {
					config.SecretEnvironment[key] = SecretReference{Name: strings.TrimSpace(typed)}
				}
			case map[string]any:
				name, _ := typed["name"].(string)
				scope, _ := typed["scope"].(string)
				if strings.TrimSpace(name) != "" {
					config.SecretEnvironment[key] = SecretReference{Scope: strings.TrimSpace(scope), Name: strings.TrimSpace(name)}
				}
			}
		}
	}
	return config, nil
}

func cloneAnyMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

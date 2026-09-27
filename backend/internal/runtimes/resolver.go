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

	return ResolvedProject{
		Context: ProjectContext{
			ProjectID:   projectID,
			ProjectName: name,
			WorkDir:     workDir.String,
			Environment: copyStringMap(config.Environment),
			Config:      cloneAnyMap(config.Raw),
		},
		Config: config,
	}, nil
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

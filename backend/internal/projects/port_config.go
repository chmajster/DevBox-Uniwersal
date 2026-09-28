package projects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

var ErrPortConfigurationBusy = errors.New("port configuration cannot change during a deployment")
var composeServiceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type PortSettings struct {
	ContainerPort      int    `json:"container_port"`
	HostPort           int    `json:"host_port"`
	HTTPSEnabled       bool   `json:"https_enabled"`
	HTTPSContainerPort int    `json:"https_container_port"`
	HTTPSHostPort      int    `json:"https_host_port"`
	ComposeService     string `json:"compose_service"`
}

type AppliedPortSettings struct {
	Settings  PortSettings             `json:"settings"`
	HTTP      providers.PublishedPort  `json:"http"`
	HTTPS     *providers.PublishedPort `json:"https,omitempty"`
	AppliedAt time.Time                `json:"applied_at"`
}

type PortConfiguration struct {
	Settings   PortSettings         `json:"settings"`
	Configured bool                 `json:"configured"`
	Applied    *AppliedPortSettings `json:"applied,omitempty"`
}

func DefaultPortSettings() PortSettings {
	return PortSettings{HostPort: 8080, HTTPSContainerPort: 443, HTTPSHostPort: 8443}
}

func ValidatePortSettings(settings PortSettings) error {
	if settings.ContainerPort < 0 || settings.ContainerPort > 65535 {
		return fmt.Errorf("%w: container_port must be 0 (automatic) or between 1 and 65535", ErrInvalidInput)
	}
	for name, value := range map[string]int{"host_port": settings.HostPort, "https_host_port": settings.HTTPSHostPort, "https_container_port": settings.HTTPSContainerPort} {
		if value < 1 || value > 65535 {
			return fmt.Errorf("%w: %s must be between 1 and 65535", ErrInvalidInput, name)
		}
	}
	if settings.HTTPSEnabled && settings.ContainerPort != 0 && settings.ContainerPort == settings.HTTPSContainerPort {
		return fmt.Errorf("%w: HTTP and HTTPS must use different container listeners", ErrInvalidInput)
	}
	if settings.ComposeService != "" && !composeServiceName.MatchString(settings.ComposeService) {
		return fmt.Errorf("%w: invalid Compose service name", ErrInvalidInput)
	}
	return nil
}

func (r *Repository) PortConfiguration(ctx context.Context, projectID string) (PortConfiguration, error) {
	project, err := r.Get(ctx, projectID)
	if err != nil {
		return PortConfiguration{}, err
	}
	result := PortConfiguration{Settings: DefaultPortSettings()}
	var desired string
	var applied sql.NullString
	var configured int
	err = r.db.QueryRowContext(ctx, `SELECT desired_json, applied_json, user_configured FROM project_port_publishing WHERE project_id=?`, projectID).Scan(&desired, &applied, &configured)
	if errors.Is(err, sql.ErrNoRows) {
		// Preserve an existing application's address until its configuration is
		// explicitly changed. New applications start from 8080, not the old
		// generic allocator's range.
		if project.Port != nil && *project.Port > 0 {
			result.Settings.HostPort = *project.Port
		}
		return result, nil
	}
	if err != nil {
		return PortConfiguration{}, err
	}
	if err := json.Unmarshal([]byte(desired), &result.Settings); err != nil {
		return PortConfiguration{}, fmt.Errorf("read desired port configuration: %w", err)
	}
	result.Configured = configured != 0
	if applied.Valid && applied.String != "" {
		var state AppliedPortSettings
		if err := json.Unmarshal([]byte(applied.String), &state); err != nil {
			return PortConfiguration{}, fmt.Errorf("read applied port configuration: %w", err)
		}
		result.Applied = &state
	}
	return result, nil
}

func (s *Service) PortConfiguration(ctx context.Context, projectID string) (PortConfiguration, error) {
	return s.repo.PortConfiguration(ctx, projectID)
}

func (s *Service) UpdatePortConfiguration(ctx context.Context, projectID string, settings PortSettings) (PortConfiguration, error) {
	settings.ComposeService = strings.TrimSpace(settings.ComposeService)
	if err := ValidatePortSettings(settings); err != nil {
		return PortConfiguration{}, err
	}
	project, err := s.repo.Get(ctx, projectID)
	if err != nil {
		return PortConfiguration{}, err
	}
	if project.ArchivedAt != nil {
		return PortConfiguration{}, fmt.Errorf("%w: archived projects cannot change published ports", ErrInvalidInput)
	}
	if settings.HTTPSEnabled {
		workDir, err := SafeWorkingDirectory(project.LocalPath, project.WorkingDirectory)
		if err != nil {
			return PortConfiguration{}, err
		}
		if !projectHasCompose(workDir) && !projectHasDockerfile(workDir) {
			return PortConfiguration{}, fmt.Errorf("%w: HTTPS requires a project Dockerfile or Compose service with a configured TLS listener and certificate; generated runtime images provide HTTP only", ErrInvalidInput)
		}
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return PortConfiguration{}, err
	}
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return PortConfiguration{}, err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM deployments d
		LEFT JOIN jobs j ON j.id = d.job_id
		WHERE d.project_id = ?
		  AND d.finished_at IS NULL
		  AND d.status NOT IN ('SUCCESS','FAILED','CANCELED','CANCELLED')
		  AND (
		    d.job_id IS NULL
		    OR j.status IN ('queued','running')
		  )
	`, projectID).Scan(&active); err != nil {
		return PortConfiguration{}, err
	}
	if active != 0 {
		return PortConfiguration{}, ErrPortConfigurationBusy
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_port_publishing(project_id,desired_json,user_configured,updated_at) VALUES(?,?,1,?) ON CONFLICT(project_id) DO UPDATE SET desired_json=excluded.desired_json,user_configured=1,updated_at=excluded.updated_at`, projectID, string(data), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return PortConfiguration{}, err
	}
	if err := tx.Commit(); err != nil {
		return PortConfiguration{}, err
	}
	return s.repo.PortConfiguration(ctx, projectID)
}

func (r *Repository) saveAppliedPorts(ctx context.Context, projectID string, state AppliedPortSettings, requested PortSettings) error {
	desired, err := json.Marshal(state.Settings)
	if err != nil {
		return err
	}
	requestedJSON, err := json.Marshal(requested)
	if err != nil {
		return err
	}
	applied, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO project_port_publishing(project_id,desired_json,applied_json,user_configured,updated_at) VALUES(?,?,?,0,?) ON CONFLICT(project_id) DO UPDATE SET desired_json=CASE WHEN project_port_publishing.desired_json=? THEN excluded.desired_json ELSE project_port_publishing.desired_json END,applied_json=excluded.applied_json,updated_at=excluded.updated_at`, projectID, string(desired), string(applied), state.AppliedAt.UTC().Format(time.RFC3339Nano), string(requestedJSON))
	return err
}

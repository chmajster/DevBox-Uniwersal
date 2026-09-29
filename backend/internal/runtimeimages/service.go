package runtimeimages

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"strings"
	"time"
)

const JobRuntimeImage = "runtime.image.action"

type Service struct {
	db      *sql.DB
	images  providers.RuntimeImageProvider
	runner  jobs.JobRunner
	audit   *audit.Service
	catalog *Catalog
}
type ProjectUse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Version struct {
	Runtime   string       `json:"runtime"`
	Version   string       `json:"version"`
	Reference string       `json:"reference"`
	Installed bool         `json:"installed"`
	ID        string       `json:"id,omitempty"`
	Size      string       `json:"size,omitempty"`
	Digest    string       `json:"digest,omitempty"`
	Projects  []ProjectUse `json:"projects"`
}

func NewService(db *sql.DB, images providers.RuntimeImageProvider, runner jobs.JobRunner, auditor *audit.Service) (*Service, error) {
	s := &Service{db: db, images: images, runner: runner, audit: auditor, catalog: NewCatalog()}
	if err := runner.Register(s); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Service) Type() string { return JobRuntimeImage }
func (s *Service) uses(ctx context.Context, runtime, version string) ([]ProjectUse, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,runtime,runtime_version FROM projects WHERE container_policy='auto'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProjectUse{}
	for rows.Next() {
		var p ProjectUse
		var rt, v string
		if err = rows.Scan(&p.ID, &p.Name, &rt, &v); err != nil {
			return nil, err
		}
		rt = containerspec.NormalizeRuntime(rt)
		if v == "" {
			v = containerspec.DefaultVersion(rt)
		}
		if rt == runtime && v == version {
			items = append(items, p)
		}
	}
	return items, rows.Err()
}
func (s *Service) List(ctx context.Context, runtime string) ([]Version, error) {
	runtime = containerspec.NormalizeRuntime(runtime)
	repo, suffix, err := containerspec.RuntimeImageCoordinates(runtime)
	if err != nil {
		return nil, err
	}
	images, err := s.images.RuntimeImages(ctx)
	if err != nil {
		return nil, err
	}
	result := []Version{}
	for _, image := range images {
		prefix := repo + ":"
		if !strings.HasPrefix(image.Reference, prefix) || !strings.HasSuffix(image.Reference, suffix) {
			continue
		}
		v := strings.TrimSuffix(strings.TrimPrefix(image.Reference, prefix), suffix)
		projects, err := s.uses(ctx, runtime, v)
		if err != nil {
			return nil, err
		}
		result = append(result, Version{Runtime: runtime, Version: v, Reference: image.Reference, Installed: true, ID: image.ID, Size: image.Size, Digest: image.Digest, Projects: projects})
	}
	return result, nil
}
func (s *Service) Enqueue(ctx context.Context, runtime, version, action string, actor, remote *string) (domain.Job, error) {
	runtime = containerspec.NormalizeRuntime(runtime)
	version = strings.TrimSpace(version)
	image, err := containerspec.BaseImage(runtime, version)
	if err != nil {
		return domain.Job{}, err
	}
	if version == "" {
		version = containerspec.DefaultVersion(runtime)
	}
	if action != "pull" && action != "remove" {
		return domain.Job{}, fmt.Errorf("image action must be pull or remove")
	}
	if action == "remove" {
		uses, err := s.uses(ctx, runtime, version)
		if err != nil {
			return domain.Job{}, err
		}
		if len(uses) > 0 {
			return domain.Job{}, fmt.Errorf("runtime version is selected by %d project(s); change their version before removing", len(uses))
		}
	}
	key := "runtime:image:" + image
	if action == "remove" {
		key = "global:maintenance"
	}
	job, err := s.runner.Enqueue(ctx, jobs.Request{Type: JobRuntimeImage, ResourceKey: key, RequestedBy: actor, Payload: map[string]any{"runtime": runtime, "version": version, "action": action}})
	if err == nil && s.audit != nil {
		_ = s.audit.Record(ctx, actor, "runtime.image."+action+".queued", "job", &job.ID, map[string]any{"image": image}, remote)
	}
	return job, err
}
func (s *Service) Run(parent context.Context, job domain.Job) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	defer cancel()
	runtime, _ := job.Payload["runtime"].(string)
	version, _ := job.Payload["version"].(string)
	action, _ := job.Payload["action"].(string)
	image, err := containerspec.BaseImage(runtime, version)
	if err != nil {
		return nil, err
	}
	log := func(stage string) {
		if l, ok := s.runner.(interface {
			Log(context.Context, string, string, string, map[string]any) error
		}); ok {
			_ = l.Log(ctx, job.ID, "info", stage, map[string]any{"image": image})
		}
	}
	log("runtime.image.preflight")
	switch action {
	case "pull":
		log("runtime.image.pulling")
		err = s.images.PullImage(ctx, image)
		if err != nil {
			return nil, err
		}
		if _, err = s.images.InspectImage(ctx, image); err != nil {
			return nil, fmt.Errorf("pulled image could not be verified: %w", err)
		}
	case "remove":
		uses, err := s.uses(ctx, runtime, version)
		if err != nil {
			return nil, err
		}
		if len(uses) > 0 {
			return nil, fmt.Errorf("runtime version is used by projects")
		}
		log("runtime.image.removing")
		if err = s.images.RemoveImage(ctx, image); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("invalid runtime image action")
	}
	log("runtime.image.verified")
	if s.audit != nil {
		_ = s.audit.Record(ctx, job.RequestedBy, "runtime.image."+action, "image", nil, map[string]any{"image": image}, nil)
	}
	return map[string]any{"image": image, "action": action}, nil
}

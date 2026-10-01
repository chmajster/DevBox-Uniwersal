package applications

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

func (h *lifecycleJobHandler) remove(ctx context.Context, app Application, source Source, workloads []Workload, endpoints []Endpoint, job domain.Job) (map[string]any, error) {
	options := DeleteOptions{RemoveContainers: true, DeleteConfiguration: true}
	raw, err := json.Marshal(job.Payload["options"])
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, fmt.Errorf("%w: delete options", ErrInvalidInput)
	}
	if options.RemoveVolumes || options.RemoveGeneratedImages {
		return nil, fmt.Errorf("%w: shared images and persistent volumes are preserved", ErrInvalidInput)
	}
	if !options.RemoveContainers && (options.DeleteConfiguration || options.RemoveSource) && len(workloads) > 0 {
		return nil, fmt.Errorf("%w: remove containers before deleting managed configuration/source", ErrConflict)
	}
	if options.RemoveSource && app.SourceType == SourceLocal {
		return nil, fmt.Errorf("%w: local source directories are never removed", ErrInvalidInput)
	}
	if options.RemoveContainers && len(workloads) > 0 {
		driver, ok := h.service.drivers.Get(app.Driver)
		if !ok {
			return nil, ErrProviderUnavailable
		}
		if err := driver.Remove(ctx, InspectRequest{Application: app, Source: source, WorkDir: h.service.workDir(app, source), Workloads: workloads, Endpoints: endpoints}); err != nil {
			return nil, err
		}
		if err := h.service.repo.ReleaseApplicationPorts(ctx, app.ID); err != nil {
			return nil, err
		}
	}
	if options.RemoveSource {
		if app.SourceType == SourceLocal {
			return nil, fmt.Errorf("%w: local source directories are never removed", ErrInvalidInput)
		}
		if app.SourceType == SourceGit || app.SourceType == SourceEmpty {
			root, err := filepath.EvalSymlinks(h.service.projectsRoot)
			if err != nil {
				return nil, err
			}
			path := h.service.workDir(app, source)
			parent, err := filepath.EvalSymlinks(filepath.Dir(path))
			if err != nil {
				return nil, err
			}
			if parent != root || filepath.Clean(path) == root {
				return nil, fmt.Errorf("%w: unsafe source path", ErrInvalidInput)
			}
			if err := os.RemoveAll(path); err != nil {
				return nil, err
			}
		}
	}
	if options.DeleteConfiguration {
		names, err := h.service.SecretNames(ctx, app.ID)
		if err != nil {
			return nil, err
		}
		if h.service.secretStore != nil {
			for _, name := range names {
				if err := h.service.secretStore.Delete(ctx, "application/"+app.ID, name); err != nil {
					return nil, err
				}
			}
		}
		if err := h.service.repo.Delete(ctx, app.ID); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true}, nil
	}
	app.DesiredState = DesiredStopped
	if err := h.service.repo.Update(ctx, app); err != nil {
		return nil, err
	}
	state, err := h.service.reconcile(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": state.Status}, nil
}

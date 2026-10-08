package applications

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Reapply the previous successful settings using retained images and volumes.
// Source Compose remains user-owned; changes to that file are not rolled back.
func (h *deployJobHandler) restoreCompose(app Application, source Source, currentID, workDir string, workloads []Workload, endpoints []Endpoint, environment map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	deployments, err := h.service.repo.Deployments(ctx, app.ID)
	if err != nil {
		return err
	}
	for _, previous := range deployments {
		if previous.ID == currentID || previous.Status != "success" || previous.Driver != "compose" {
			continue
		}
		var plan DeploymentPlan
		if err := json.Unmarshal(previous.PlanSnapshot, &plan); err != nil {
			return err
		}
		driver, ok := h.service.drivers.Get("compose")
		if !ok {
			return ErrProviderUnavailable
		}
		result, err := driver.Deploy(ctx, ExecutionRequest{Restore: true, Recreate: true, Application: app, Source: source, WorkDir: workDir, Deployment: previous, Workloads: workloads, Endpoints: endpoints, SensitiveEnvironment: environment}, plan)
		if err != nil {
			return fmt.Errorf("restore previous Compose plan: %w", err)
		}
		if err := h.service.repo.ReplaceTopology(ctx, app.ID, workloads, endpoints); err != nil {
			return err
		}
		for _, workload := range workloads {
			if state, ok := result.Resources[workload.Name]; ok {
				_ = h.service.repo.UpdateWorkloadObserved(ctx, workload.ID, state.ResourceID, state.ObservedState, state.HealthState, state.Image)
			}
		}
		for _, endpoint := range endpoints {
			if port, ok := result.EndpointPorts[endpoint.Name]; ok {
				_ = h.service.repo.UpdateEndpointRuntime(ctx, endpoint.ID, &port, ObservedRunning)
			}
		}
		if plan.Runtime != nil {
			_ = h.service.repo.SaveRuntime(ctx, *plan.Runtime)
		}
		_, err = h.service.reconcile(ctx, app.ID)
		return err
	}
	return fmt.Errorf("no previous successful Compose plan is available")
}

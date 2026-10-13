package applications

import (
	"context"
	"errors"
	"strings"
	"time"
)

func (s *Service) reconcile(ctx context.Context, id string) (ApplicationState, error) {
	app, err := s.repo.Get(ctx, id)
	if err != nil {
		return ApplicationState{}, err
	}
	source, err := s.repo.Source(ctx, id)
	if err != nil {
		return ApplicationState{}, err
	}
	workloads, err := s.repo.Workloads(ctx, id)
	if err != nil {
		return ApplicationState{}, err
	}
	endpoints, err := s.repo.Endpoints(ctx, id)
	if err != nil {
		return ApplicationState{}, err
	}
	if len(workloads) == 0 {
		observed := ObservedUnknown
		if app.ObservedState == ObservedFailed {
			observed = ObservedFailed
		}
		if app.DesiredState == DesiredStopped {
			observed = ObservedStopped
		}
		_ = s.repo.UpdateApplicationState(ctx, id, observed, HealthUnknown)
		return ApplicationState{ApplicationID: id, DesiredState: app.DesiredState, ObservedState: observed, HealthState: HealthUnknown, Status: visibleStatus(Application{ObservedState: observed, DesiredState: app.DesiredState}, nil, nil), Workloads: workloads, CheckedAt: time.Now().UTC()}, nil
	}
	driver, ok := s.drivers.Get(app.Driver)
	if !ok {
		return ApplicationState{}, ErrProviderUnavailable
	}
	observed, err := driver.Inspect(ctx, InspectRequest{Application: app, Source: source, WorkDir: s.workDir(app, source), Workloads: workloads, Endpoints: endpoints})
	if err != nil {
		// An unavailable provider is not evidence that last-known green state
		// remains valid. Keep resource identities but invalidate observations.
		for _, w := range workloads {
			_ = s.repo.UpdateWorkloadObserved(ctx, w.ID, w.DriverResourceID, ObservedUnknown, HealthUnknown, w.Image)
		}
		_ = s.repo.UpdateApplicationState(ctx, id, ObservedUnknown, HealthUnknown)
		for _, e := range endpoints {
			_ = s.repo.UpdateEndpointRuntime(ctx, e.ID, e.HostPort, ObservedUnknown)
		}
		return ApplicationState{}, err
	}
	byName := map[string]ObservedWorkload{}
	for _, item := range observed {
		byName[item.Name] = item
	}
	for i := range workloads {
		current, ok := byName[workloads[i].Name]
		if !ok {
			current = ObservedWorkload{Name: workloads[i].Name, ResourceID: workloads[i].DriverResourceID, ObservedState: ObservedMissing, HealthState: HealthUnknown}
		}
		if current.ResourceID == "" && current.ObservedState == ObservedUnknown {
			current.ObservedState = ObservedMissing
			current.HealthState = HealthUnhealthy
		}
		workloads[i].DriverResourceID = current.ResourceID
		if current.Image != "" {
			workloads[i].Image = current.Image
		}
		workloads[i].ObservedState = current.ObservedState
		workloads[i].HealthState = current.HealthState
		if checker, ok := s.logs.(interface {
			CheckApplicationHTTP(context.Context, int, string, string) error
		}); ok && current.ObservedState == ObservedRunning {
			for _, endpoint := range endpoints {
				if endpoint.WorkloadID != workloads[i].ID || endpoint.HostPort == nil || !endpoint.Public || (endpoint.Protocol != "http" && endpoint.Protocol != "https") {
					continue
				}
				checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				err := checker.CheckApplicationHTTP(checkCtx, *endpoint.HostPort, endpoint.Protocol, endpoint.HealthPath)
				cancel()
				if err != nil {
					workloads[i].HealthState = HealthUnhealthy
				} else if workloads[i].HealthState != HealthUnhealthy {
					workloads[i].HealthState = HealthHealthy
				}
			}
		}
		if err := s.repo.UpdateWorkloadObserved(ctx, workloads[i].ID, workloads[i].DriverResourceID, workloads[i].ObservedState, workloads[i].HealthState, workloads[i].Image); err != nil {
			return ApplicationState{}, err
		}
	}
	observedState, healthState := aggregateObserved(workloads), aggregateHealth(workloads)
	if err := s.repo.UpdateApplicationState(ctx, id, observedState, healthState); err != nil {
		return ApplicationState{}, err
	}
	for _, endpoint := range endpoints {
		status := ObservedUnknown
		for _, workload := range workloads {
			if workload.ID == endpoint.WorkloadID {
				status = workload.ObservedState
				if status == ObservedRunning && workload.HealthState == HealthUnhealthy {
					status = "unhealthy"
				}
				current := byName[workload.Name]
				if status == ObservedRunning && endpoint.Public && endpoint.HostPort != nil && current.PortBindings != nil {
					published := false
					for _, binding := range current.PortBindings {
						if binding.ContainerPort == endpoint.ContainerPort && binding.HostPort == *endpoint.HostPort {
							published = true
						}
					}
					if !published {
						status = ObservedMissing
					}
				}
				break
			}
		}
		_ = s.repo.UpdateEndpointRuntime(ctx, endpoint.ID, endpoint.HostPort, status)
	}
	return ApplicationState{ApplicationID: id, DesiredState: app.DesiredState, ObservedState: observedState, HealthState: healthState, Status: AggregateStatus(app.DesiredState, workloads), Workloads: workloads, CheckedAt: time.Now().UTC()}, nil
}

func AggregateStatus(desired string, workloads []Workload) string {
	if len(workloads) == 0 {
		if desired == DesiredStopped {
			return "stopped"
		}
		return "unknown"
	}
	primaryFailure := false
	anyFailure := false
	anyStarting := false
	allStopped := true
	allRunning := true
	for _, w := range workloads {
		failed := w.ObservedState == ObservedFailed || w.ObservedState == ObservedMissing || w.ObservedState == ObservedExited || w.HealthState == HealthUnhealthy
		if failed {
			anyFailure = true
			if w.Primary {
				primaryFailure = true
			}
		}
		if w.ObservedState == ObservedStarting {
			anyStarting = true
		}
		if w.ObservedState != ObservedStopped && w.ObservedState != ObservedExited && w.ObservedState != ObservedMissing {
			allStopped = false
		}
		if w.ObservedState != ObservedRunning {
			allRunning = false
		}
	}
	if desired == DesiredStopped && allStopped {
		return "stopped"
	}
	if primaryFailure {
		for _, w := range workloads {
			if w.Primary && (w.ObservedState == ObservedFailed || w.HealthState == HealthUnhealthy) {
				return "failed"
			}
		}
		return "failed"
	}
	if anyFailure {
		return "failed"
	}
	if anyStarting {
		return "starting"
	}
	if allRunning {
		return "running"
	}
	return "unknown"
}

func aggregateObserved(workloads []Workload) string {
	if len(workloads) == 0 {
		return ObservedUnknown
	}
	allRunning, allStopped, anyStarting := true, true, false
	for _, w := range workloads {
		if w.ObservedState == ObservedFailed {
			return ObservedFailed
		}
		if w.ObservedState == ObservedStarting {
			anyStarting = true
		}
		if w.ObservedState != ObservedRunning {
			allRunning = false
		}
		if w.ObservedState != ObservedStopped && w.ObservedState != ObservedExited && w.ObservedState != ObservedMissing {
			allStopped = false
		}
	}
	if anyStarting {
		return ObservedStarting
	}
	if allRunning {
		return ObservedRunning
	}
	if allStopped {
		return ObservedStopped
	}
	for _, w := range workloads {
		if w.ObservedState == ObservedMissing {
			return ObservedMissing
		}
	}
	return ObservedUnknown
}

func aggregateHealth(workloads []Workload) string {
	if len(workloads) == 0 {
		return HealthUnknown
	}
	known := false
	secondaryProblem := false
	for _, w := range workloads {
		if w.HealthState == HealthUnhealthy {
			if w.Primary {
				return HealthUnhealthy
			}
			secondaryProblem = true
			known = true
		}
		if w.HealthState == HealthHealthy {
			known = true
		}
		if w.ObservedState == ObservedMissing || w.ObservedState == ObservedFailed {
			if w.Primary {
				return HealthUnhealthy
			}
			secondaryProblem = true
		}
	}
	if secondaryProblem {
		return HealthDegraded
	}
	if known {
		return HealthHealthy
	}
	return HealthUnknown
}

func (s *Service) ReconcileAll(ctx context.Context, autoHeal bool) error {
	apps, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	for _, app := range apps {
		active, err := s.repo.ActiveOperation(ctx, app.ID)
		if err != nil || active != nil {
			continue
		}
		state, err := s.reconcile(ctx, app.ID)
		if err != nil {
			if errors.Is(err, ErrProviderUnavailable) {
				continue
			}
			continue
		}
		if autoHeal && app.AutoStart && app.DesiredState == DesiredRunning && (state.ObservedState == ObservedMissing || state.ObservedState == ObservedStopped) {
			_, _, _ = s.EnqueueDeploy(ctx, app.ID, nil)
		}
	}
	return nil
}

func (s *Service) RunReconciler(ctx context.Context, interval time.Duration) {
	if interval < 2*time.Second {
		interval = 5 * time.Second
	}
	_ = s.ReconcileAll(ctx, true)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.ReconcileAll(ctx, false)
		}
	}
}

func stateMissing(state string) bool {
	return strings.EqualFold(state, ObservedMissing) || strings.EqualFold(state, ObservedUnknown)
}

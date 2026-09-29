package providers

import "context"

type deploymentObserverKey struct{}

// WithDeploymentObserver scopes progress to one orchestration call. The provider
// reports a stage before doing the corresponding work; persistence stays owned
// by the orchestration module, and an observer failure aborts activation.
func WithDeploymentObserver(ctx context.Context, observer func(string) error) context.Context {
	return context.WithValue(ctx, deploymentObserverKey{}, observer)
}
func ReportDeploymentStage(ctx context.Context, stage string) error {
	if observer, ok := ctx.Value(deploymentObserverKey{}).(func(string) error); ok && observer != nil {
		return observer(stage)
	}
	return nil
}

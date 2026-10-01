package applications

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type DetectRequest struct {
	SourceType string
	Source Source
	WorkDir string
	Configuration map[string]any
}

type PlanRequest struct {
	Application Application
	Source Source
	WorkDir string
	Detection DetectionResult
	Configuration map[string]any
	SourceRevision string
}

type ExecutionRequest struct {
	Application Application
	Source Source
	WorkDir string
	Deployment Deployment
}

type InspectRequest struct {
	Application Application
	Source Source
	WorkDir string
	Workloads []Workload
	Endpoints []Endpoint
}

type DeploymentResult struct {
	Resources map[string]ResourceState `json:"resources"`
	EndpointPorts map[string]int `json:"endpoint_ports,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type ResourceState struct {
	ResourceID string `json:"resource_id"`
	Image string `json:"image,omitempty"`
	ObservedState string `json:"observed_state"`
	HealthState string `json:"health_state"`
}

type ObservedWorkload struct {
	Name string `json:"name"`
	ResourceID string `json:"resource_id,omitempty"`
	Image string `json:"image,omitempty"`
	ObservedState string `json:"observed_state"`
	HealthState string `json:"health_state"`
}

type DeploymentDriver interface {
	Name() string
	Detect(context.Context, DetectRequest) (DetectionResult, error)
	Plan(context.Context, PlanRequest) (DeploymentPlan, error)
	Deploy(context.Context, ExecutionRequest, DeploymentPlan) (DeploymentResult, error)
	Inspect(context.Context, InspectRequest) ([]ObservedWorkload, error)
	Start(context.Context, InspectRequest) error
	Stop(context.Context, InspectRequest) error
	Restart(context.Context, InspectRequest) error
	Remove(context.Context, InspectRequest) error
}

type DriverRegistry struct {
	mu sync.RWMutex
	drivers map[string]DeploymentDriver
}

func NewDriverRegistry() *DriverRegistry {
	return &DriverRegistry{drivers: map[string]DeploymentDriver{}}
}

func (r *DriverRegistry) Register(driver DeploymentDriver) error {
	if driver == nil || driver.Name() == "" { return fmt.Errorf("%w: driver name is required", ErrInvalidInput) }
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[driver.Name()]; exists { return fmt.Errorf("%w: driver %q is already registered", ErrConflict, driver.Name()) }
	r.drivers[driver.Name()] = driver
	return nil
}

func (r *DriverRegistry) Get(name string) (DeploymentDriver, bool) {
	r.mu.RLock(); defer r.mu.RUnlock()
	driver, ok := r.drivers[name]
	return driver, ok
}

func (r *DriverRegistry) Names() []string {
	r.mu.RLock(); defer r.mu.RUnlock()
	out := make([]string, 0, len(r.drivers))
	for name := range r.drivers { out = append(out, name) }
	sort.Strings(out)
	return out
}

type PortAllocator interface {
	Reserve(ctx context.Context, applicationID, endpointID, purpose string, preferred *int) (providers.PortLease, error)
	Release(ctx context.Context, applicationID, endpointID string, port int) error
}

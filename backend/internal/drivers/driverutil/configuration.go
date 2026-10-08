package driverutil

import (
	"context"
	"fmt"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
)

func Modules(config map[string]any) []containerspec.Module {
	var out []containerspec.Module
	for _, name := range ConfigStringSlice(config, "modules") {
		out = append(out, containerspec.Module{Name: name})
	}
	return out
}

func ConfigureEndpoint(endpoint applications.PlannedEndpoint, config map[string]any) applications.PlannedEndpoint {
	if port := ConfigInt(config, "container_port"); port > 0 {
		endpoint.ContainerPort = port
	}
	if port := ConfigInt(config, "host_port"); port > 0 {
		endpoint.HostPort = port
	}
	if protocol := ConfigString(config, "protocol"); protocol != "" {
		endpoint.Protocol = protocol
	}
	if path := ConfigString(config, "health_path"); path != "" {
		endpoint.HealthPath = path
	}
	endpoint.Domain = ConfigString(config, "domain")
	endpoint.TLSMode = ConfigString(config, "tls_mode")
	return endpoint
}

func RollbackPort(ports applications.PortAllocator, request applications.ExecutionRequest, endpoint applications.Endpoint, port int) {
	// A failed redeployment never frees the lease serving the previous container.
	if endpoint.HostPort != nil && *endpoint.HostPort == port {
		return
	}
	_ = ports.Release(context.Background(), request.Application.ID, endpoint.ID, port)
}

func ApplyListener(spec containerspec.DeploymentSpec, endpoint applications.PlannedEndpoint, managed bool) (containerspec.DeploymentSpec, error) {
	if managed {
		if endpoint.Protocol != "http" {
			return spec, fmt.Errorf("%w: managed runtimes use HTTP; use Existing Compose for TCP/TLS", applications.ErrInvalidInput)
		}
		return containerspec.WithContainerPort(spec, endpoint.ContainerPort)
	}
	// The Dockerfile owns its listener. This changes publishing, not source code.
	spec.ContainerPort = endpoint.ContainerPort
	return spec, nil
}

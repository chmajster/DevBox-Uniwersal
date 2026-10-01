package driverutil

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func Observed(info providers.ContainerInfo) (string, string) {
	state := strings.ToLower(strings.TrimSpace(info.State))
	observed := applications.ObservedUnknown
	switch state {
	case "running":
		observed = applications.ObservedRunning
	case "created", "restarting":
		observed = applications.ObservedStarting
	case "exited", "dead":
		observed = applications.ObservedExited
	case "paused":
		observed = applications.ObservedStopped
	case "removing":
		observed = applications.ObservedStarting
	case "":
		observed = applications.ObservedUnknown
	default:
		observed = state
	}
	health := applications.HealthUnknown
	switch strings.ToLower(strings.TrimSpace(info.Health)) {
	case "healthy":
		health = applications.HealthHealthy
	case "unhealthy":
		health = applications.HealthUnhealthy
	case "starting":
		health = applications.HealthUnknown
	}
	return observed, health
}

func MissingError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not found") || strings.Contains(message, "no such container")
}

func Labels(application applications.Application, deployment applications.Deployment, workload string) map[string]string {
	return map[string]string{
		"io.devbox.managed":          "true",
		"io.devbox.application.id":   application.ID,
		"io.devbox.application.slug": application.Slug,
		"io.devbox.workload.name":    workload,
		"io.devbox.deployment.id":    deployment.ID,
	}
}

func MergeLabels(target map[string]string, values map[string]string) map[string]string {
	if target == nil {
		target = map[string]string{}
	}
	for key, value := range values {
		target[key] = value
	}
	delete(target, "io.devbox.project")
	return target
}

func Endpoint(request applications.ExecutionRequest, name string) (applications.Endpoint, error) {
	for _, item := range request.Endpoints {
		if item.Name == name {
			return item, nil
		}
	}
	if len(request.Endpoints) == 1 {
		return request.Endpoints[0], nil
	}
	return applications.Endpoint{}, fmt.Errorf("%w: persisted endpoint %q not found", applications.ErrInvalidInput, name)
}

func Workload(request applications.ExecutionRequest, name string) (applications.Workload, error) {
	for _, item := range request.Workloads {
		if item.Name == name {
			return item, nil
		}
	}
	if len(request.Workloads) == 1 {
		return request.Workloads[0], nil
	}
	return applications.Workload{}, fmt.Errorf("%w: persisted workload %q not found", applications.ErrInvalidInput, name)
}

func ConfigInt(config map[string]any, key string) int {
	switch value := config[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

func ConfigString(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return strings.TrimSpace(value)
}

func ConfigStringMap(config map[string]any, key string) map[string]string {
	result := map[string]string{}
	raw, ok := config[key]
	if !ok {
		return result
	}
	switch typed := raw.(type) {
	case map[string]string:
		for k, v := range typed {
			result[k] = v
		}
	case map[string]any:
		for k, v := range typed {
			if text, ok := v.(string); ok {
				result[k] = text
			}
		}
	}
	return result
}

func ConfigStringSlice(config map[string]any, key string) []string {
	raw, ok := config[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := []string{}
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

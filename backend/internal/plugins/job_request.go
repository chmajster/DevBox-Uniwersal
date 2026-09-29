package plugins

import "github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"

func newInstallRequest(component string, extensions []string, actor *string) jobs.Request {
	return jobs.Request{Type: JobInstall, RequestedBy: actor, Payload: map[string]any{"component": component, "extensions": extensions}}
}

package applications

// HostingFields exposes the application settings as a simple read model. Its
// values come from normalized source/runtime/endpoint records, not a second
// competing set of persisted configuration fields.
type HostingFields struct {
	SourcePath        string  `json:"source_path"`
	RuntimeMode       string  `json:"runtime_mode"`
	RuntimeType       string  `json:"runtime_type"`
	RuntimeVersion    string  `json:"runtime_version"`
	StartCommand      string  `json:"start_command"`
	ContainerPort     int     `json:"container_port"`
	HostPort          *int    `json:"host_port"`
	Domain            *string `json:"domain"`
	SSLEnabled        bool    `json:"ssl_enabled"`
	DockerProjectName string  `json:"docker_project_name"`
}

func (s *Service) hostingFields(app Application, source Source, runtime *Runtime, endpoints []Endpoint) HostingFields {
	config := decodeConfiguration(app.SourceConfig)
	fields := HostingFields{SourcePath: s.workDir(app, source), RuntimeMode: configString(config, "deployment_mode"), RuntimeType: configString(config, "runtime"), RuntimeVersion: configString(config, "runtime_version"), StartCommand: configString(config, "start_command"), DockerProjectName: DockerProjectName(app.ID)}
	if fields.RuntimeMode == "" {
		fields.RuntimeMode = "auto"
		if app.Driver == "compose" {
			fields.RuntimeMode = "compose"
		}
	}
	if runtime != nil {
		fields.RuntimeType = runtime.Name
		fields.RuntimeVersion = runtime.Version
	}
	for _, endpoint := range endpoints {
		if endpoint.Primary {
			fields.ContainerPort = endpoint.ContainerPort
			fields.HostPort = endpoint.HostPort
			fields.Domain = endpoint.Domain
			fields.SSLEnabled = endpoint.TLSMode == "existing"
			break
		}
	}
	return fields
}

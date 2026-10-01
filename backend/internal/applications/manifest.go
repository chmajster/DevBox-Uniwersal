package applications

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Manifest struct {
	Version         int
	ApplicationName string
	Driver          string
	Workloads       map[string]ManifestWorkload
	Endpoints       map[string]ManifestEndpoint
	Health          ManifestHealth
	Environment     map[string]string
	Secrets         []string
}

type ManifestWorkload struct {
	Service string
	Role    string
	Primary bool
}

type ManifestEndpoint struct {
	Workload      string
	Protocol      string
	ContainerPort int
	Public        bool
	Primary       bool
	HealthPath    string
}

type ManifestHealth struct {
	Workload string
	Type     string
	Path     string
}

func LoadManifest(workDir string) (*Manifest, error) {
	path := filepath.Join(workDir, "devbox.yaml")
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read devbox.yaml: %w", err)
	}
	manifest, err := ParseManifest(string(content))
	if err != nil {
		return nil, fmt.Errorf("devbox.yaml: %w", err)
	}
	return manifest, nil
}

// ParseManifest intentionally implements a constrained declarative subset of YAML.
// It accepts only scalar values and the documented DevBox sections. Arbitrary
// YAML tags, aliases, inline objects and executable shell fields are rejected.
func ParseManifest(content string) (*Manifest, error) {
	m := &Manifest{Version: 1, Workloads: map[string]ManifestWorkload{}, Endpoints: map[string]ManifestEndpoint{}, Environment: map[string]string{}}
	scanner := bufio.NewScanner(strings.NewReader(content))
	section, item := "", ""
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := strings.TrimRight(scanner.Text(), " \t\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.ContainsAny(trimmed, "&*!{}[]") {
			return nil, fmt.Errorf("line %d uses unsupported YAML syntax", lineNo)
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if strings.Contains(raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))], "\t") || indent%2 != 0 || indent > 4 {
			return nil, fmt.Errorf("line %d uses tab indentation", lineNo)
		}
		if section == "secrets" && indent == 2 {
			if !strings.HasPrefix(trimmed, "- ") {
				return nil, fmt.Errorf("line %d: secrets must be a scalar list", lineNo)
			}
			secret := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if secret == "" {
				return nil, fmt.Errorf("line %d: empty secret name", lineNo)
			}
			m.Secrets = append(m.Secrets, secret)
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, fmt.Errorf("line %d must contain key: value", lineNo)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		value = strings.Trim(value, "'\"")
		if strings.EqualFold(key, "command") || strings.EqualFold(key, "entrypoint") || strings.EqualFold(key, "script") || strings.EqualFold(key, "shell") {
			return nil, fmt.Errorf("line %d contains forbidden executable field %q", lineNo, key)
		}

		if indent == 0 {
			item = ""
			if value == "" {
				section = key
				switch section {
				case "application", "deployment", "workloads", "endpoints", "health", "environment", "secrets":
				default:
					return nil, fmt.Errorf("line %d contains unsupported section %q", lineNo, section)
				}
				continue
			}
			section = ""
			if key != "version" {
				return nil, fmt.Errorf("line %d contains unsupported root key %q", lineNo, key)
			}
			version, err := strconv.Atoi(value)
			if err != nil || version != 1 {
				return nil, fmt.Errorf("line %d: version must be 1", lineNo)
			}
			m.Version = version
			continue
		}

		if (section == "workloads" || section == "endpoints") && indent == 2 && value == "" {
			item = key
			continue
		}

		switch section {
		case "application":
			if key != "name" {
				return nil, fmt.Errorf("line %d: unsupported application key %q", lineNo, key)
			}
			m.ApplicationName = value
		case "deployment":
			if key != "driver" {
				return nil, fmt.Errorf("line %d: unsupported deployment key %q", lineNo, key)
			}
			switch value {
			case "compose", "dockerfile", "managed", "image":
			default:
				return nil, fmt.Errorf("line %d: unsupported driver %q", lineNo, value)
			}
			m.Driver = value
		case "workloads":
			if item == "" {
				return nil, fmt.Errorf("line %d: workload name is required", lineNo)
			}
			current := m.Workloads[item]
			switch key {
			case "service":
				current.Service = value
			case "role":
				current.Role = value
			case "primary":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("line %d: primary must be boolean", lineNo)
				}
				current.Primary = parsed
			default:
				return nil, fmt.Errorf("line %d: unsupported workload key %q", lineNo, key)
			}
			m.Workloads[item] = current
		case "endpoints":
			if item == "" {
				return nil, fmt.Errorf("line %d: endpoint name is required", lineNo)
			}
			current := m.Endpoints[item]
			switch key {
			case "workload":
				current.Workload = value
			case "protocol":
				switch value {
				case "http", "https", "tcp":
				default:
					return nil, fmt.Errorf("line %d: unsupported protocol %q", lineNo, value)
				}
				current.Protocol = value
			case "container_port":
				port, err := strconv.Atoi(value)
				if err != nil || port < 1 || port > 65535 {
					return nil, fmt.Errorf("line %d: invalid container_port", lineNo)
				}
				current.ContainerPort = port
			case "public":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("line %d: public must be boolean", lineNo)
				}
				current.Public = parsed
			case "primary":
				parsed, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("line %d: primary must be boolean", lineNo)
				}
				current.Primary = parsed
			case "health_path":
				current.HealthPath = value
			default:
				return nil, fmt.Errorf("line %d: unsupported endpoint key %q", lineNo, key)
			}
			m.Endpoints[item] = current
		case "health":
			switch key {
			case "workload":
				m.Health.Workload = value
			case "type":
				m.Health.Type = value
			case "path":
				m.Health.Path = value
			default:
				return nil, fmt.Errorf("line %d: unsupported health key %q", lineNo, key)
			}
		case "environment":
			if key == "" {
				return nil, fmt.Errorf("line %d: environment key is required", lineNo)
			}
			m.Environment[key] = value
		default:
			return nil, fmt.Errorf("line %d is outside a supported section", lineNo)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	primaryWorkloads, primaryEndpoints := 0, 0
	for name, workload := range m.Workloads {
		if workload.Role == "" {
			workload.Role = "internal"
			m.Workloads[name] = workload
		}
		if workload.Primary {
			primaryWorkloads++
		}
	}
	for _, endpoint := range m.Endpoints {
		if endpoint.Primary {
			primaryEndpoints++
		}
		if endpoint.Workload == "" || endpoint.ContainerPort == 0 {
			return nil, fmt.Errorf("endpoint requires workload and container_port")
		}
	}
	if primaryWorkloads > 1 || primaryEndpoints > 1 {
		return nil, fmt.Errorf("only one primary workload and one primary endpoint are allowed")
	}
	return m, nil
}

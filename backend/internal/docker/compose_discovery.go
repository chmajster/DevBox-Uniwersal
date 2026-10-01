package docker

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type composeDiscoveryService struct {
	Image  string            `json:"image"`
	Labels map[string]string `json:"labels"`
	Ports  []struct {
		Target    int             `json:"target"`
		Published json.RawMessage `json:"published"`
		Protocol  string          `json:"protocol"`
	} `json:"ports"`
	Expose []any `json:"expose"`
}

type scoredComposePortCandidate struct {
	candidate providers.ComposePortCandidate
	score     int
}

type composeDiscoveryServiceInfo struct {
	name           string
	service        composeDiscoveryService
	infrastructure bool
	targets        map[int]int
}

var composeInfrastructureNames = map[string]bool{
	"mysql": true, "mariadb": true, "postgres": true, "postgresql": true,
	"redis": true, "mongo": true, "mongodb": true, "elasticsearch": true,
	"rabbitmq": true, "memcached": true, "database": true, "db": true, "cache": true,
}

var composeInfrastructurePorts = map[int]bool{
	3306: true, 5432: true, 6379: true, 27017: true,
	9200: true, 9300: true, 5672: true, 11211: true,
}

var composeApplicationPorts = map[int]bool{
	80: true, 443: true, 3000: true, 3001: true, 4000: true, 5000: true,
	5173: true, 8000: true, 8080: true, 8081: true, 8443: true,
	8888: true, 9000: true,
}

var composeApplicationServiceNames = map[string]bool{
	"app": true, "application": true, "web": true, "frontend": true,
	"backend": true, "api": true, "server": true, "www": true,
	"php": true, "nginx": true, "apache": true,
}

func (p *CLIProvider) DiscoverComposeApplicationPorts(ctx context.Context, directory, projectName, healthcheck string) (providers.ComposePortDiscovery, error) {
	// Discovery must describe the project source, not a previous DevBox port/database
	// override. Otherwise a removed service can survive in normalized topology and
	// make a stale selection look valid.
	data, err := p.originalComposeConfigJSON(ctx, directory, projectName)
	if err != nil {
		return providers.ComposePortDiscovery{}, err
	}
	dockerfilePort := dockerfileExposePortForDiscovery(filepath.Join(directory, "Dockerfile"))
	return discoverComposeApplicationPorts(data, healthcheck, dockerfilePort)
}

func discoverComposeApplicationPorts(data []byte, healthcheck string, dockerfilePort int) (providers.ComposePortDiscovery, error) {
	var config struct {
		Services map[string]composeDiscoveryService `json:"services"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return providers.ComposePortDiscovery{}, fmt.Errorf("decode normalized compose config: %w", err)
	}
	sum := sha256.Sum256(data)
	result := providers.ComposePortDiscovery{
		Candidates:     []providers.ComposePortCandidate{},
		Infrastructure: []providers.ComposePortCandidate{},
		Fingerprint:    hex.EncodeToString(sum[:]),
	}

	services := make([]composeDiscoveryServiceInfo, 0, len(config.Services))
	for name, service := range config.Services {
		targets := composeDiscoveryTargets(service)
		infra := composeServiceIsInfrastructure(name, service.Image, targets)
		services = append(services, composeDiscoveryServiceInfo{name: name, service: service, infrastructure: infra, targets: targets})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].name < services[j].name })

	var healthHost string
	var healthPort int
	var healthProtocol string
	if parsed, err := url.Parse(strings.TrimSpace(healthcheck)); err == nil && parsed.Host != "" {
		healthHost = strings.ToLower(parsed.Hostname())
		healthProtocol = strings.ToLower(parsed.Scheme)
		if parsed.Port() != "" {
			healthPort, _ = strconv.Atoi(parsed.Port())
		} else if healthProtocol == "https" {
			healthPort = 443
		} else if healthProtocol == "http" {
			healthPort = 80
		}
	}

	scored := make([]scoredComposePortCandidate, 0)
	for _, info := range services {
		if info.infrastructure {
			for port, hostPort := range info.targets {
				result.Infrastructure = append(result.Infrastructure, providers.ComposePortCandidate{
					Service: info.name, ContainerPort: port, HostPort: hostPort,
					Protocol: composeCandidateProtocol(port, ""), Source: "docker-compose.yml",
				})
			}
			continue
		}

		if healthHost == strings.ToLower(info.name) && healthPort > 0 && healthPort <= 65535 {
			hostPort := info.targets[healthPort]
			scored = append(scored, scoredComposePortCandidate{
				candidate: providers.ComposePortCandidate{
					Service: info.name, ContainerPort: healthPort, HostPort: hostPort,
					Protocol: composeCandidateProtocol(healthPort, healthProtocol), Source: "healthcheck",
				},
				score: 1000,
			})
		}

		for port, hostPort := range info.targets {
			score := 10
			if composeApplicationServiceNames[strings.ToLower(strings.TrimSpace(info.name))] {
				score += 100
			}
			if composeServiceHasApplicationLabel(info.service.Labels) {
				score += 200
			}
			if composeApplicationPorts[port] {
				if port == 443 || port == 8443 {
					score += 50
				} else {
					score += 60
				}
			}
			scored = append(scored, scoredComposePortCandidate{
				candidate: providers.ComposePortCandidate{
					Service: info.name, ContainerPort: port, HostPort: hostPort,
					Protocol: composeCandidateProtocol(port, ""), Source: "docker-compose.yml",
				},
				score: score,
			})
		}
	}

	if len(scored) == 0 && dockerfilePort > 0 {
		service := composeDockerfileFallbackService(services)
		if service != "" {
			scored = append(scored, scoredComposePortCandidate{
				candidate: providers.ComposePortCandidate{
					Service: service, ContainerPort: dockerfilePort,
					Protocol: composeCandidateProtocol(dockerfilePort, ""), Source: "Dockerfile EXPOSE",
				},
				score: 40,
			})
		}
	}

	// Deduplicate exact service/container-port candidates. A healthcheck source
	// wins over a generic Compose declaration for the same listener.
	dedup := map[string]scoredComposePortCandidate{}
	for _, item := range scored {
		key := item.candidate.Service + ":" + strconv.Itoa(item.candidate.ContainerPort)
		current, ok := dedup[key]
		if !ok || item.score > current.score {
			dedup[key] = item
		}
	}
	scored = scored[:0]
	for _, item := range dedup {
		scored = append(scored, item)
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].candidate.Service != scored[j].candidate.Service {
			return scored[i].candidate.Service < scored[j].candidate.Service
		}
		return scored[i].candidate.ContainerPort < scored[j].candidate.ContainerPort
	})
	sort.Slice(result.Infrastructure, func(i, j int) bool {
		if result.Infrastructure[i].Service != result.Infrastructure[j].Service {
			return result.Infrastructure[i].Service < result.Infrastructure[j].Service
		}
		return result.Infrastructure[i].ContainerPort < result.Infrastructure[j].ContainerPort
	})

	for _, item := range scored {
		result.Candidates = append(result.Candidates, item.candidate)
	}
	if len(scored) == 1 || (len(scored) > 1 && scored[0].score > scored[1].score) {
		selected := scored[0].candidate
		result.Selected = &selected
	}
	return result, nil
}

func composeDiscoveryTargets(service composeDiscoveryService) map[int]int {
	targets := map[int]int{}
	for _, port := range service.Ports {
		protocol := strings.ToLower(strings.TrimSpace(port.Protocol))
		if protocol != "" && protocol != "tcp" {
			continue
		}
		if port.Target < 1 || port.Target > 65535 {
			continue
		}
		hostPort := composeJSONPort(port.Published)
		if hostPort < 1 || hostPort > 65535 {
			hostPort = 0
		}
		if current, exists := targets[port.Target]; !exists || current == 0 {
			targets[port.Target] = hostPort
		}
	}
	for _, raw := range service.Expose {
		text := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
		if strings.HasSuffix(text, "/udp") {
			continue
		}
		text = strings.TrimSuffix(text, "/tcp")
		port, err := strconv.Atoi(text)
		if err == nil && port > 0 && port <= 65535 {
			if _, exists := targets[port]; !exists {
				targets[port] = 0
			}
		}
	}
	return targets
}

func composeServiceIsInfrastructure(name, image string, targets map[int]int) bool {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	if composeInfrastructureNames[lowerName] {
		return true
	}
	for _, token := range strings.FieldsFunc(lowerName, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	}) {
		if composeInfrastructureNames[token] {
			return true
		}
	}
	imageName := composeImageBaseName(image)
	if composeInfrastructureNames[imageName] {
		return true
	}
	if len(targets) == 0 {
		return false
	}
	for port := range targets {
		if !composeInfrastructurePorts[port] {
			return false
		}
	}
	return true
}

func composeImageBaseName(image string) string {
	value := strings.ToLower(strings.TrimSpace(image))
	if at := strings.Index(value, "@"); at >= 0 {
		value = value[:at]
	}
	if slash := strings.LastIndex(value, "/"); slash >= 0 {
		value = value[slash+1:]
	}
	if colon := strings.LastIndex(value, ":"); colon >= 0 {
		value = value[:colon]
	}
	return value
}

func composeServiceHasApplicationLabel(labels map[string]string) bool {
	for key, value := range labels {
		k := strings.ToLower(strings.TrimSpace(key))
		v := strings.ToLower(strings.TrimSpace(value))
		switch k {
		case "devbox.application", "devbox.main", "com.devbox.application":
			if v == "1" || v == "true" || v == "yes" || v == "application" || v == "web" || v == "main" {
				return true
			}
		case "devbox.role", "com.devbox.role":
			if v == "application" || v == "web" || v == "main" {
				return true
			}
		}
	}
	return false
}

func composeCandidateProtocol(port int, hint string) string {
	if strings.EqualFold(hint, "https") || port == 443 || port == 8443 {
		return "https"
	}
	return "http"
}

func composeDockerfileFallbackService(services []composeDiscoveryServiceInfo) string {
	eligible := make([]string, 0)
	preferred := make([]string, 0)
	for _, info := range services {
		if info.infrastructure {
			continue
		}
		eligible = append(eligible, info.name)
		if composeApplicationServiceNames[strings.ToLower(strings.TrimSpace(info.name))] {
			preferred = append(preferred, info.name)
		}
	}
	if len(preferred) == 1 {
		return preferred[0]
	}
	if len(eligible) == 1 {
		return eligible[0]
	}
	return ""
}

func dockerfileExposePortForDiscovery(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) < len("EXPOSE ") || !strings.EqualFold(line[:len("EXPOSE ")], "EXPOSE ") {
			continue
		}
		for _, field := range strings.Fields(line[len("EXPOSE "):]) {
			lower := strings.ToLower(field)
			if strings.HasSuffix(lower, "/udp") {
				continue
			}
			value := strings.TrimSuffix(lower, "/tcp")
			port, err := strconv.Atoi(value)
			if err == nil && port > 0 && port <= 65535 {
				return port
			}
		}
	}
	return 0
}

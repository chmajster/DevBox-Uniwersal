package managed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/applications"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/drivers/driverutil"
)

func customMode(sourceType string, config map[string]any) string {
	if sourceType == applications.SourceImage {
		return "image"
	}
	mode := driverutil.ConfigString(config, "deployment_mode")
	if mode == "dockerfile" || mode == "image" {
		return mode
	}
	return ""
}

// Dockerfile inspection supplies defaults; ambiguous ports always require a choice.
func customDefaults(root string) (string, []int, error) {
	file, err := filepath.EvalSymlinks(filepath.Join(root, "Dockerfile"))
	if err != nil {
		return "", nil, err
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(base, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("Dockerfile escapes source")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", nil, err
	}
	workdir := ""
	ports := []int{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "FROM":
			workdir, ports = "", nil
		case "WORKDIR":
			if strings.ContainsAny(fields[1], "$\\") {
				continue
			}
			workdir = path.Join(workdir, fields[1])
		case "EXPOSE":
			for _, item := range fields[1:] {
				if port, err := strconv.Atoi(strings.TrimSuffix(item, "/tcp")); err == nil && port > 0 && port <= 65535 {
					ports = append(ports, port)
				}
			}
		}
	}
	return workdir, ports, nil
}

func (d *Driver) detectCustom(request applications.DetectRequest, mode string) (applications.DetectionResult, error) {
	port := driverutil.ConfigInt(request.Configuration, "container_port")
	result := applications.DetectionResult{Driver: d.Name(), Runtime: mode, Profile: mode, Confidence: "high", Services: []applications.ServiceDetection{{Name: "web", SuggestedRole: "web", Primary: true, Confidence: "high"}}}
	if mode == "dockerfile" {
		workdir, ports, err := customDefaults(request.WorkDir)
		if err != nil {
			return result, fmt.Errorf("%w: Dockerfile: %v", applications.ErrInvalidInput, err)
		}
		if port == 0 && len(ports) == 1 {
			port = ports[0]
		}
		if workdir == "" && driverutil.ConfigString(request.Configuration, "mount_target") == "" {
			result.RequiresConfiguration = true
			result.Warnings = append(result.Warnings, "Dockerfile has no absolute WORKDIR; choose mount_target")
		}
	} else if request.Source.DockerImage == "" {
		return result, fmt.Errorf("%w: docker_image is required", applications.ErrInvalidInput)
	}
	if port == 0 {
		result.RequiresConfiguration = true
		result.Warnings = append(result.Warnings, "choose the container HTTP port")
	} else {
		result.Endpoints = []applications.EndpointDetection{{Service: "web", Protocol: "http", ContainerPort: port, Primary: true, Confidence: "high"}}
	}
	return result, nil
}

func (d *Driver) specification(id, root, revision string, source applications.Source, runtime, version, profile string, config map[string]any, hostPort int) (containerspec.DeploymentSpec, error) {
	mode := profile
	if mode != "dockerfile" && mode != "image" {
		spec, err := containerspec.GenerateManagedProfile(id, root, runtime, version, driverutil.Modules(config), revision, hostPort, profile)
		if err != nil {
			return spec, err
		}
		spec.WorkingDirectory = driverutil.ConfigString(config, "working_directory")
		spec.RestartPolicy = driverutil.ConfigString(config, "restart_policy")
		if runtime == "php" && profile != "wordpress" {
			if documentRoot := driverutil.ConfigString(config, "document_root"); documentRoot != "" {
				if info, err := os.Stat(filepath.Join(root, documentRoot)); err != nil || !info.IsDir() {
					return spec, fmt.Errorf("document_root must be an existing source directory")
				}
				encoded, _ := json.Marshal([]string{"sh", "-c", "sed -i 's@DocumentRoot .*@DocumentRoot /app/" + documentRoot + "@' /etc/apache2/sites-available/000-default.conf"})
				spec.Dockerfile += "RUN " + string(encoded) + "\n"
			}
		}
		digest := sha256.Sum256([]byte(spec.Fingerprint + "\n" + spec.Dockerfile))
		spec.Fingerprint = hex.EncodeToString(digest[:])
		spec.Image = strings.Split(spec.Image, ":")[0] + ":" + spec.Fingerprint[:16]
		return spec, nil
	}
	spec := containerspec.DeploymentSpec{ProjectID: id, Runtime: "custom", ContainerName: "devbox-app-" + strings.TrimRight(id[:min(24, len(id))], "-"), HostPort: hostPort, ContainerPort: driverutil.ConfigInt(config, "container_port"), Environment: map[string]string{}, BindMounts: map[string]string{}, Labels: map[string]string{}, RestartPolicy: driverutil.ConfigString(config, "restart_policy"), WorkingDirectory: driverutil.ConfigString(config, "working_directory"), Capabilities: []string{"NET_BIND_SERVICE", "SETUID", "SETGID"}}
	if mode == "image" {
		spec.Image = source.DockerImage
		spec.Capabilities = append(spec.Capabilities, "CHOWN")
		return spec, nil
	}
	workdir, ports, err := customDefaults(root)
	if err != nil {
		return spec, err
	}
	if spec.ContainerPort == 0 && len(ports) == 1 {
		spec.ContainerPort = ports[0]
	}
	target := driverutil.ConfigString(config, "mount_target")
	if target == "" {
		target = workdir
	}
	if !path.IsAbs(target) || target == "/" {
		return spec, applications.ErrConfigurationRequired
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return spec, err
	}
	spec.ContextDir, spec.DockerfilePath = abs, filepath.Join(abs, "Dockerfile")
	spec.BindMounts[abs] = target
	data, err := os.ReadFile(spec.DockerfilePath)
	if err != nil {
		return spec, err
	}
	digest := sha256.Sum256(append(data, []byte(revision)...))
	spec.Fingerprint = hex.EncodeToString(digest[:])
	spec.Image = "devbox/dockerfile-" + id + ":" + spec.Fingerprint[:16]
	for manifest, dependency := range map[string]string{"package.json": "node_modules", "composer.json": "vendor"} {
		if _, err := os.Stat(filepath.Join(abs, manifest)); err == nil {
			spec.AnonymousVolumes = append(spec.AnonymousVolumes, path.Join(target, dependency))
		}
	}
	return spec, nil
}

func (d *Driver) ensureBaseImages(ctx context.Context, spec containerspec.DeploymentSpec) error {
	provider, ok := d.engine.(interface {
		EnsureImage(context.Context, string) error
	})
	if !ok {
		return nil
	}
	if spec.Runtime == "custom" && spec.DockerfilePath == "" {
		return provider.EnsureImage(ctx, spec.Image)
	}
	if spec.Dockerfile == "" {
		return nil
	} // Docker builds validate custom multistage Dockerfiles.
	for _, line := range strings.Split(spec.Dockerfile, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "FROM") {
			if err := provider.EnsureImage(ctx, fields[1]); err != nil {
				return fmt.Errorf("runtime image %s is unavailable: %w", fields[1], err)
			}
		}
	}
	return nil
}

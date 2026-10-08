package containerspec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// WithContainerPort changes a generated listener as well as its publishing
// target. Custom images retain their own entrypoint; their application must
// already listen on the chosen port. Zero preserves auto detection.
func WithContainerPort(spec DeploymentSpec, port int) (DeploymentSpec, error) {
	if port < 0 || port > 65535 {
		return DeploymentSpec{}, fmt.Errorf("container port must be between 1 and 65535, or 0 for automatic detection")
	}
	if port == 0 || port == spec.ContainerPort {
		return spec, nil
	}
	old := strconv.Itoa(spec.ContainerPort)
	next := strconv.Itoa(port)
	spec.ContainerPort = port
	if spec.Runtime == "custom" {
		return spec, nil
	}
	if spec.Dockerfile == "" {
		return DeploymentSpec{}, fmt.Errorf("cannot configure listener without a managed Dockerfile")
	}
	// Only rewrite known generated listener directives, not source files or
	// arbitrary occurrences in runtime versions, commands, or module names.
	spec.Dockerfile = strings.NewReplacer(
		"0.0.0.0:"+old, "0.0.0.0:"+next,
		"--port "+old, "--port "+next,
		"PORT="+old, "PORT="+next,
		"EXPOSE "+old+"\n", "EXPOSE "+next+"\n",
		"Listen "+old, "Listen "+next,
		"*:"+old, "*:"+next,
	).Replace(spec.Dockerfile)
	if spec.Runtime == "static" {
		// EXPOSE alone does not change nginx's listener. The base image runs as
		// uid 101; root is used only during the image build to edit its config.
		spec.Dockerfile += "USER root\nRUN sed -i 's/" + old + "/" + next + "/g' /etc/nginx/conf.d/default.conf\nUSER 101\n"
	}
	env := make(map[string]string, len(spec.Environment))
	for key, value := range spec.Environment {
		if key == "PORT" || key == "APP_PORT" {
			value = next
		}
		env[key] = value
	}
	spec.Environment = env
	digest := sha256.Sum256([]byte(spec.Fingerprint + "\ncontainer-port=" + next + "\n" + spec.Dockerfile))
	spec.Fingerprint = hex.EncodeToString(digest[:])
	separator := strings.LastIndex(spec.Image, ":")
	if separator < 0 {
		return DeploymentSpec{}, fmt.Errorf("managed image is missing its fingerprint tag")
	}
	spec.Image = spec.Image[:separator+1] + spec.Fingerprint[:16]
	labels := make(map[string]string, len(spec.Labels)+1)
	for key, value := range spec.Labels {
		labels[key] = value
	}
	labels["io.devbox.fingerprint"] = spec.Fingerprint
	spec.Labels = labels
	return spec, nil
}

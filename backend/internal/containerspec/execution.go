package containerspec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// ExecutionOptions affect only generated managed runtimes, never project-owned files.
type ExecutionOptions struct {
	SourceMode    string      `json:"source_mode,omitempty"`
	UID           int         `json:"uid,omitempty"`
	GID           int         `json:"gid,omitempty"`
	CGO           bool        `json:"cgo,omitempty"`
	BuildOutputs  []string    `json:"build_outputs,omitempty"`
	WritablePaths []string    `json:"writable_paths,omitempty"`
	Healthcheck   Healthcheck `json:"healthcheck,omitempty"`
}

type Healthcheck struct {
	Target           string `json:"target,omitempty"`
	StartupSeconds   int    `json:"startup_seconds,omitempty"`
	TimeoutSeconds   int    `json:"timeout_seconds,omitempty"`
	ExpectedStatuses []int  `json:"expected_statuses,omitempty"`
}

var relativeOutput = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)

func ValidateExecution(o ExecutionOptions) error {
	if o.SourceMode != "" && o.SourceMode != "live" && o.SourceMode != "versioned" {
		return fmt.Errorf("source_mode must be live or versioned")
	}
	if o.UID < 0 || o.UID > 2147483647 || o.GID < 0 || o.GID > 2147483647 || (o.UID == 0) != (o.GID == 0) {
		return fmt.Errorf("UID and GID must both be non-root positive IDs, or both 0 to use the image defaults")
	}
	if len(o.BuildOutputs) > 20 || len(o.WritablePaths) > 20 {
		return fmt.Errorf("at most 20 build outputs or writable paths are allowed")
	}
	for _, p := range append(append([]string{}, o.BuildOutputs...), o.WritablePaths...) {
		if !relativeOutput.MatchString(p) || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../") {
			return fmt.Errorf("paths must be safe relative application directories: %q", p)
		}
		for _, part := range strings.Split(p, "/") {
			if part == ".git" || strings.HasPrefix(part, ".env") {
				return fmt.Errorf("secret/VCS directories cannot be runtime artifacts")
			}
		}
	}
	if err := ValidateHealthcheck(o.Healthcheck); err != nil {
		return err
	}
	return nil
}

func ValidateHealthcheck(h Healthcheck) error {
	if h.StartupSeconds < 0 || h.StartupSeconds > 600 || h.TimeoutSeconds < 0 || h.TimeoutSeconds > 30 {
		return fmt.Errorf("healthcheck startup must be 1..600 seconds and request timeout 1..30 (0 uses defaults)")
	}
	for _, s := range h.ExpectedStatuses {
		if s < 100 || s > 599 {
			return fmt.Errorf("healthcheck HTTP status must be between 100 and 599")
		}
	}
	if len(h.ExpectedStatuses) > 30 {
		return fmt.Errorf("too many expected HTTP statuses")
	}
	if strings.ContainsAny(h.Target, "\r\n\x00") {
		return fmt.Errorf("invalid healthcheck target")
	}
	t := strings.TrimSpace(h.Target)
	if t == "" || t == "tcp" {
		return nil
	}
	if strings.ContainsAny(t, "\r\n\x00") {
		return fmt.Errorf("invalid healthcheck target")
	}
	u, err := url.Parse(t)
	if err != nil {
		return fmt.Errorf("invalid healthcheck target")
	}
	if strings.HasPrefix(t, "/") && !strings.HasPrefix(t, "//") && u.Host == "" {
		return nil
	}
	if (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "tcp") || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("healthcheck must be an HTTP path or a local HTTP/HTTPS/TCP URL")
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
	default:
		return fmt.Errorf("deployment healthcheck must target the local application; use a path such as /health")
	}
	return nil
}

// HealthcheckURL preserves the configured protocol/path/query but always tests
// the actual published port. It must not accidentally probe another project.
func HealthcheckURL(h Healthcheck, hostPort int) (string, error) {
	if err := ValidateHealthcheck(h); err != nil {
		return "", err
	}
	if hostPort < 1 || hostPort > 65535 {
		return "", fmt.Errorf("invalid published healthcheck port")
	}
	target := strings.TrimSpace(h.Target)
	scheme, p, q := "http", "/", ""
	if target == "tcp" {
		scheme = "tcp"
	} else if target != "" {
		u, _ := url.Parse(target)
		if u.Scheme != "" {
			scheme = u.Scheme
		}
		if u.Path != "" {
			p = u.Path
		}
		q = u.RawQuery
	}
	u := url.URL{Scheme: scheme, Host: "127.0.0.1:" + strconv.Itoa(hostPort), Path: p, RawQuery: q}
	return u.String(), nil
}

func WithExecution(spec DeploymentSpec, o ExecutionOptions, build, start, legacyHealth string) (DeploymentSpec, error) {
	if err := ValidateExecution(o); err != nil {
		return DeploymentSpec{}, err
	}
	spec.Healthcheck = o.Healthcheck
	if spec.Healthcheck.Target == "" {
		spec.Healthcheck.Target = strings.TrimSpace(legacyHealth)
	}
	if err := ValidateHealthcheck(spec.Healthcheck); err != nil {
		return DeploymentSpec{}, err
	}
	if spec.Runtime == "custom" {
		if strings.TrimSpace(build) != "" || strings.TrimSpace(start) != "" || o.CGO || o.UID != 0 || len(o.BuildOutputs) > 0 || len(o.WritablePaths) > 0 || o.SourceMode == "versioned" {
			return DeploymentSpec{}, fmt.Errorf("custom Dockerfile controls commands, user and source: configure these in Dockerfile instead of managed runtime options")
		}
		return spec, nil
	}
	if spec.Runtime == "go" && (len(o.BuildOutputs) > 0 || len(o.WritablePaths) > 0) {
		return DeploymentSpec{}, fmt.Errorf("Go stores its executable at /app; configure persistent data paths in a custom Dockerfile")
	}
	if o.CGO && spec.Runtime != "go" {
		return DeploymentSpec{}, fmt.Errorf("CGO is available only for Go")
	}
	if o.SourceMode == "versioned" {
		spec.BindMounts = nil
		spec.AnonymousVolumes = nil
		delete(spec.Labels, "io.devbox.live-source")
	}
	root := "/app"
	if spec.Runtime == "static" {
		root = "/usr/share/nginx/html"
	}
	spec.WritablePaths = nil
	for _, p := range o.WritablePaths {
		spec.WritablePaths = append(spec.WritablePaths, path.Join(root, p))
	}
	outputs := o.BuildOutputs
	if spec.Runtime == "node" && outputs == nil {
		outputs = []string{"dist", "build", ".next", ".nuxt", ".output", "out"}
	}
	if len(spec.BindMounts) > 0 {
		for _, out := range outputs {
			spec.AnonymousVolumes = append(spec.AnonymousVolumes, path.Join(root, out))
		}
	}
	d := spec.Dockerfile
	build = strings.TrimSpace(build)
	start = strings.TrimSpace(start)
	if strings.ContainsRune(build, 0) || strings.ContainsRune(start, 0) {
		return DeploymentSpec{}, fmt.Errorf("commands cannot contain NUL")
	}
	if len(build) > 8192 || len(start) > 8192 {
		return DeploymentSpec{}, fmt.Errorf("command exceeds 8192 characters")
	}
	run := func(command string) string {
		b, _ := json.Marshal([]string{"sh", "-lc", command})
		return "RUN " + string(b) + "\n"
	}
	if build != "" {
		switch spec.Runtime {
		case "node":
			d = strings.Replace(d, "RUN if [ -f package.json ]; then npm run build --if-present; fi\n", run(build), 1)
		case "go":
			d = strings.Replace(d, "RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/app .\n", run("set -e; mkdir -p /out; "+build+"; test -x /out/app"), 1)
		case "static":
			d += "USER root\nWORKDIR " + root + "\n" + run(build) + "USER 101\n"
		default:
			d = strings.Replace(d, "RUN useradd", run(build)+"RUN useradd", 1)
		}
	}
	if spec.Runtime == "go" {
		if o.CGO {
			d = strings.Replace(d, "CGO_ENABLED=0", "CGO_ENABLED=1", 1)
			d = strings.Replace(d, "WORKDIR /src\n", "ENV CGO_ENABLED=1\nWORKDIR /src\n", 1)
		}
		if o.CGO || start != "" {
			// Same Debian release as the builder provides the matching dynamic loader.
			d = strings.Replace(d, "FROM gcr.io/distroless/static-debian12:nonroot\n", "FROM debian:bookworm-slim\nRUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libstdc++6 && rm -rf /var/lib/apt/lists/*\nUSER 65532:65532\n", 1)
		}
	}
	if start != "" {
		b, _ := json.Marshal([]string{"sh", "-lc", start})
		if spec.Runtime == "go" {
			d = strings.Replace(d, "ENTRYPOINT [\"/app\"]\n", "ENTRYPOINT []\n", 1)
		}
		d += "CMD " + string(b) + "\n"
	}
	if spec.Runtime != "go" {
		uid := "10001:0"
		if spec.Runtime == "node" {
			uid = "1000:1000"
		}
		if spec.Runtime == "static" {
			uid = "101:101"
		}
		if o.UID != 0 {
			uid = fmt.Sprintf("%d:%d", o.UID, o.GID)
			spec.User = uid
		}
		dirs := []string{}
		for _, out := range outputs {
			dirs = append(dirs, path.Join(root, out))
		}
		for _, out := range o.WritablePaths {
			dirs = append(dirs, path.Join(root, out))
		}
		d += "USER root\n"
		if len(dirs) > 0 {
			d += "RUN mkdir -p " + strings.Join(dirs, " ") + "\n"
		}
		// This affects image content (including fresh anonymous volumes), not host files.
		d += "RUN chown -R " + uid + " " + root + "\nUSER " + uid + "\n"
	} else if o.UID != 0 {
		spec.User = fmt.Sprintf("%d:%d", o.UID, o.GID)
		d += "USER " + spec.User + "\n"
	}
	spec.Dockerfile = d
	encoded, _ := json.Marshal(o)
	h := sha256.Sum256([]byte(spec.Fingerprint + "\nexecution-v1\n" + string(encoded) + "\n" + d))
	spec.Fingerprint = hex.EncodeToString(h[:])
	sep := strings.LastIndex(spec.Image, ":")
	if sep < 0 {
		return DeploymentSpec{}, fmt.Errorf("managed image has no tag")
	}
	spec.Image = spec.Image[:sep+1] + spec.Fingerprint[:16]
	spec.Labels["io.devbox.fingerprint"] = spec.Fingerprint
	spec.Labels["io.devbox.source-mode"] = "live"
	if o.SourceMode == "versioned" {
		spec.Labels["io.devbox.source-mode"] = "versioned"
	}
	return spec, nil
}

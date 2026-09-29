package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

const JobInstall = "plugin.install"

type installLogger interface {
	Log(context.Context, string, string, string, map[string]any) error
}
type InstallHandler struct {
	service *Service
	logger  installLogger
	audit   *audit.Service
}

func NewInstallHandler(s *Service, l installLogger, a *audit.Service) *InstallHandler {
	return &InstallHandler{s, l, a}
}
func (h *InstallHandler) Type() string { return JobInstall }
func validateInstall(component string, extensions []string) error {
	switch component {
	case "docker-compose", "php-fpm", "mysql", "postgresql":
		if len(extensions) > 0 {
			return fmt.Errorf("extensions are valid only for php-extensions")
		}
	case "php-extensions":
		if len(extensions) < 1 || len(extensions) > len(phpExtensionCatalog) {
			return fmt.Errorf("select supported PHP extensions")
		}
		for _, id := range extensions {
			found := false
			for _, d := range phpExtensionCatalog {
				if d.ID == id {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("unsupported PHP extension %q", id)
			}
		}
	default:
		return fmt.Errorf("unsupported system component")
	}
	return nil
}
func (h *InstallHandler) Run(parent context.Context, j domain.Job) (map[string]any, error) {
	component, _ := j.Payload["component"].(string)
	var extensions []string
	encoded, _ := json.Marshal(j.Payload["extensions"])
	_ = json.Unmarshal(encoded, &extensions)
	if err := validateInstall(component, extensions); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	log := func(line string) {
		if h.logger != nil {
			_ = h.logger.Log(ctx, j.ID, "info", line, nil)
		}
	}
	ctx = context.WithValue(ctx, installLogKey{}, log)
	log("plugin.preflight: " + component)
	var result any
	var err error
	log("plugin.installing: " + component)
	switch component {
	case "docker-compose":
		result, err = h.service.InstallDockerCompose(ctx)
	case "php-fpm":
		result, err = h.service.InstallPHPFPM(ctx)
	case "postgresql":
		result, err = h.service.InstallPostgreSQL(ctx)
	case "mysql":
		result, err = h.service.InstallMySQL(ctx)
	case "php-extensions":
		result, err = h.service.InstallPHPExtensions(ctx, extensions)
	}
	if err != nil {
		return nil, err
	}
	log("plugin.verified: " + component)
	if h.audit != nil {
		_ = h.audit.Record(ctx, j.RequestedBy, "plugin."+component+".installed", "plugin", nil, map[string]any{"job_id": j.ID, "extensions": extensions}, nil)
	}
	return map[string]any{"component": component, "status": result}, nil
}
func (m *Module) enqueueInstall(w http.ResponseWriter, r *http.Request, component string, extensions []string) {
	if err := validateInstall(component, extensions); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_install", err.Error())
		return
	}
	if m.runner == nil {
		writeError(w, http.StatusServiceUnavailable, "jobs_unavailable", "Job Engine is unavailable")
		return
	}
	if m.service.helperBinary == "" {
		writeError(w, http.StatusServiceUnavailable, "helper_unavailable", "Privileged helper is not configured")
		return
	}
	var actor *string
	if u, ok := api.CurrentUser(r.Context()); ok {
		id := u.ID
		actor = &id
	}
	job, err := m.runner.Enqueue(r.Context(), newInstallRequest(component, extensions, actor))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueue_failed", err.Error())
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "plugin."+component+".install.queued", "job", &job.ID, nil, nil)
	}
	writeData(w, http.StatusAccepted, job)
}

type installLogKey struct{}
type installOutput struct {
	mu      sync.Mutex
	pending string
	tail    string
	log     func(string)
}

var installCredentialURL = regexp.MustCompile(`(https?://)[^/@\s]+@`)

func (w *installOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	text := installCredentialURL.ReplaceAllString(string(b), "${1}[redacted]@")
	w.tail += text
	if len(w.tail) > 4096 {
		w.tail = w.tail[len(w.tail)-4096:]
	}
	w.pending += text
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		if w.log != nil && strings.TrimSpace(line) != "" {
			w.log(line)
		}
	}
	if len(w.pending) > 8192 {
		if w.log != nil {
			w.log(w.pending[:8192] + " [truncated]")
		}
		w.pending = ""
	}
	return len(b), nil
}
func (s *Service) runPackageInstall(ctx context.Context, component string) error {
	if s.helperBinary == "" {
		return fmt.Errorf("privileged helper is not configured")
	}
	sudo := s.sudoBinary
	if sudo == "" {
		sudo = "sudo"
	}
	cmd := exec.CommandContext(ctx, sudo, "-n", s.helperBinary, "install-package", component)
	log, _ := ctx.Value(installLogKey{}).(func(string))
	output := &installOutput{log: log}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("package installation failed: %s: %w", strings.TrimSpace(output.tail), err)
	}
	if output.pending != "" && log != nil {
		log(output.pending)
	}
	return nil
}

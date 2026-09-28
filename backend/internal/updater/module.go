package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

const (
	defaultRepository = "https://github.com/chmajster/DevBox-Uniwersal.git"
	defaultRef        = "main"
)

var gitRefPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

type Status struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Repository      string `json:"repository"`
	Ref             string `json:"ref"`
	AutoUpdate      bool   `json:"auto_update"`
	Schedule        string `json:"schedule"`
	LastError       string `json:"last_error,omitempty"`
	CheckedAt       string `json:"checked_at"`
}

type Service struct {
	currentVersion string
	helperBinary   string
	sudoBinary     string
	repository     string
	ref            string
}

func NewService(currentVersion, helperBinary, sudoBinary string) *Service {
	return &Service{
		currentVersion: strings.TrimSpace(currentVersion),
		helperBinary:   strings.TrimSpace(helperBinary),
		sudoBinary:     strings.TrimSpace(sudoBinary),
		repository:     defaultRepository,
		ref:            defaultRef,
	}
}

func (s *Service) Status(ctx context.Context) Status {
	status := Status{
		CurrentVersion: s.currentVersion,
		Repository:     s.repository,
		Ref:            s.ref,
		Schedule:       "co 12 godzin",
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if status.CurrentVersion == "" {
		status.CurrentVersion = "unknown"
	}
	status.AutoUpdate = timerEnabled(ctx)
	latest, err := remoteSHA(ctx, s.repository, s.ref)
	if err != nil {
		status.LastError = err.Error()
		return status
	}
	status.LatestVersion = latest
	status.UpdateAvailable = latest != "" && latest != status.CurrentVersion
	return status
}

func (s *Service) Apply(ctx context.Context) error {
	if s.helperBinary == "" {
		return errors.New("privileged helper is not configured")
	}
	sudo := s.sudoBinary
	if sudo == "" {
		sudo = "sudo"
	}
	cmd := exec.CommandContext(ctx, sudo, s.helperBinary, "start-update")
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("start update: %s", message)
	}
	return nil
}

func remoteSHA(ctx context.Context, repository, ref string) (string, error) {
	if !gitRefPattern.MatchString(ref) {
		return "", errors.New("invalid update ref")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "git", "ls-remote", repository, "refs/heads/"+ref)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote failed: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 1 || len(fields[0]) != 40 {
		return "", errors.New("remote ref did not return a commit SHA")
	}
	return fields[0], nil
}

func timerEnabled(ctx context.Context) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "systemctl", "is-enabled", "devbox-update.timer")
	return cmd.Run() == nil
}

type Module struct {
	service *Service
	audit   *audit.Service
}

func NewModule(service *Service, auditService *audit.Service) *Module {
	return &Module{service: service, audit: auditService}
}

func (m *Module) Name() string { return "updater" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	secure := func(role domain.Role, handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(role, handler))
	}
	mux.Handle("GET /api/v1/update/status", secure(domain.RoleAdmin, m.status))
	mux.Handle("POST /api/v1/update/apply", secure(domain.RoleAdmin, m.apply))
}

func (m *Module) status(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.Status(r.Context()))
}

func (m *Module) apply(w http.ResponseWriter, r *http.Request) {
	if err := m.service.Apply(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "update_start_failed", err.Error())
		return
	}
	actor := actorID(r)
	_ = m.audit.Record(r.Context(), actor, "system.update.start", "system", nil, map[string]any{"source": defaultRepository, "ref": defaultRef}, nil)
	writeData(w, http.StatusAccepted, map[string]string{
		"status":  "started",
		"message": "Aktualizacja została uruchomiona w tle. Usługa DevBox zrestartuje się po poprawnym wdrożeniu.",
	})
}

func actorID(r *http.Request) *string {
	user, ok := api.CurrentUser(r.Context())
	if !ok {
		return nil
	}
	id := user.ID
	return &id
}

func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

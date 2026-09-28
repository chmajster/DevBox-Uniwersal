package updater

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

const (
	defaultRepository   = "https://github.com/chmajster/DevBox-Uniwersal.git"
	defaultRef          = "main"
	defaultProgressFile = "/var/lib/devbox/update-status"
)

var gitRefPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

type Status struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version,omitempty"`
	LatestCommitAt  string `json:"latest_commit_at,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	Repository      string `json:"repository"`
	Ref             string `json:"ref"`
	AutoUpdate      bool   `json:"auto_update"`
	Schedule        string `json:"schedule"`
	LastError       string `json:"last_error,omitempty"`
	CheckedAt       string `json:"checked_at"`
}

type Progress struct {
	State          string `json:"state"`
	Percent        int    `json:"percent"`
	Stage          string `json:"stage"`
	Message        string `json:"message,omitempty"`
	CurrentVersion string `json:"current_version,omitempty"`
	TargetVersion  string `json:"target_version,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	FinishedAt     string `json:"finished_at,omitempty"`
	Error          string `json:"error,omitempty"`
}

func (p Progress) Active() bool {
	return p.State == "starting" || p.State == "running"
}

type Service struct {
	currentVersion string
	helperBinary   string
	sudoBinary     string
	repository     string
	ref            string
	progressFile   string
}

func NewService(currentVersion, helperBinary, sudoBinary string) *Service {
	progressFile := strings.TrimSpace(os.Getenv("DEVBOX_UPDATE_PROGRESS_FILE"))
	if progressFile == "" {
		progressFile = defaultProgressFile
	}
	return &Service{
		currentVersion: strings.TrimSpace(currentVersion),
		helperBinary:   strings.TrimSpace(helperBinary),
		sudoBinary:     strings.TrimSpace(sudoBinary),
		repository:     defaultRepository,
		ref:            defaultRef,
		progressFile:   progressFile,
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
	latest, commitAt, err := remoteCommitInfo(ctx, s.repository, s.ref)
	if err != nil {
		status.LastError = err.Error()
		return status
	}
	status.LatestVersion = latest
	status.LatestCommitAt = commitAt
	status.UpdateAvailable = latest != "" && latest != status.CurrentVersion
	return status
}

func (s *Service) Progress(ctx context.Context) Progress {
	progress, err := readProgressFile(s.progressFile)
	if err == nil {
		if updateServiceActive(ctx) && !progress.Active() {
			progress.State = "starting"
			progress.Stage = "starting"
			progress.Percent = 1
			progress.Message = "Usługa aktualizacji została uruchomiona i przygotowuje wykonanie."
			progress.FinishedAt = ""
			progress.Error = ""
			progress.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		}
		return progress
	}
	if errors.Is(err, os.ErrNotExist) {
		if updateServiceActive(ctx) {
			return Progress{
				State:          "starting",
				Percent:        1,
				Stage:          "starting",
				Message:        "Usługa aktualizacji została uruchomiona i przygotowuje wykonanie.",
				CurrentVersion: s.currentVersion,
				UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
			}
		}
		return Progress{State: "idle", Stage: "idle", CurrentVersion: s.currentVersion}
	}
	return Progress{
		State:          "unknown",
		Stage:          "unknown",
		CurrentVersion: s.currentVersion,
		Error:          err.Error(),
	}
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

func readProgressFile(path string) (Progress, error) {
	file, err := os.Open(path)
	if err != nil {
		return Progress{}, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return Progress{}, fmt.Errorf("read update progress: %w", err)
	}

	percent, _ := strconv.Atoi(values["PERCENT"])
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	state := values["STATE"]
	if state == "" {
		state = "unknown"
	}
	stage := values["STAGE"]
	if stage == "" {
		stage = "unknown"
	}
	return Progress{
		State:          state,
		Percent:        percent,
		Stage:          stage,
		Message:        values["MESSAGE"],
		CurrentVersion: values["CURRENT_VERSION"],
		TargetVersion:  values["TARGET_VERSION"],
		StartedAt:      values["STARTED_AT"],
		UpdatedAt:      values["UPDATED_AT"],
		FinishedAt:     values["FINISHED_AT"],
		Error:          values["ERROR"],
	}, nil
}

func remoteCommitInfo(ctx context.Context, repository, ref string) (string, string, error) {
	if !gitRefPattern.MatchString(ref) {
		return "", "", errors.New("invalid update ref")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	tmp, err := os.MkdirTemp("", "devbox-update-check-*")
	if err != nil {
		return "", "", fmt.Errorf("create update check directory: %w", err)
	}
	defer os.RemoveAll(tmp)

	if out, err := exec.CommandContext(checkCtx, "git", "-C", tmp, "init", "-q").CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("git init failed: %s", strings.TrimSpace(string(out)))
	}
	fetch := exec.CommandContext(checkCtx, "git", "-C", tmp, "fetch", "--depth=1", "--no-tags", repository, "refs/heads/"+ref)
	if out, err := fetch.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return "", "", fmt.Errorf("git fetch failed: %s", message)
	}

	show := exec.CommandContext(checkCtx, "git", "-C", tmp, "show", "-s", "--format=%H%n%cI", "FETCH_HEAD")
	out, err := show.Output()
	if err != nil {
		return "", "", fmt.Errorf("git show failed: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 || len(lines[0]) != 40 {
		return "", "", errors.New("remote commit metadata is incomplete")
	}
	if _, err := time.Parse(time.RFC3339, lines[1]); err != nil {
		return "", "", fmt.Errorf("parse remote commit date: %w", err)
	}
	return lines[0], lines[1], nil
}

func timerEnabled(ctx context.Context) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "systemctl", "is-enabled", "devbox-update.timer")
	return cmd.Run() == nil
}

func updateServiceActive(ctx context.Context) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "systemctl", "is-active", "--quiet", "devbox-update.service")
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
	mux.Handle("GET /api/v1/update/progress", secure(domain.RoleAdmin, m.progress))
	mux.Handle("POST /api/v1/update/apply", secure(domain.RoleAdmin, m.apply))
}

func (m *Module) status(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.Status(r.Context()))
}

func (m *Module) progress(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.Progress(r.Context()))
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
		"message": "Aktualizacja została uruchomiona w tle. Postęp i aktualny etap są dostępne na tej stronie.",
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

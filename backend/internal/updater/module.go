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
	defaultUpdateLog    = "/var/log/devbox-update.log"
	updateLogTailLines  = 40
	updateLogTailBytes  = 32 * 1024
)

var (
	gitRefPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+package updater

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

)
	sensitiveAssignmentPattern = regexp.MustCompile(`(?i)(password|passwd|token|secret|api[_-]?key)([[:space:]]*[:=][[:space:]]*)([^[:space:]]+)`)
	authorizationHeaderPattern = regexp.MustCompile(`(?i)(authorization[[:space:]]*:[[:space:]]*).+package updater

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
	defaultUpdateLog    = "/var/log/devbox-update.log"
	updateLogTailLines  = 40
	updateLogTailBytes  = 32 * 1024
)

var (
	gitRefPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+package updater

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

)
	)
	bearerPattern = regexp.MustCompile(`(?i)(bearer[[:space:]]+)[A-Za-z0-9._~+/=-]+`)
	mysqlPasswordPattern = regexp.MustCompile(`(?i)(-p|--password=)([^[:space:]]+)`)
)

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
	State          string   `json:"state"`
	Percent        int      `json:"percent"`
	Stage          string   `json:"stage"`
	Message        string   `json:"message,omitempty"`
	CurrentVersion string   `json:"current_version,omitempty"`
	TargetVersion  string   `json:"target_version,omitempty"`
	StartedAt      string   `json:"started_at,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
	FinishedAt     string   `json:"finished_at,omitempty"`
	Error          string   `json:"error,omitempty"`
	ExitCode       *int     `json:"exit_code,omitempty"`
	LogPath        string   `json:"log_path,omitempty"`
	LogTail        []string `json:"log_tail,omitempty"`
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
	updateLogFile  string
}

func NewService(currentVersion, helperBinary, sudoBinary string) *Service {
	progressFile := strings.TrimSpace(os.Getenv("DEVBOX_UPDATE_PROGRESS_FILE"))
	if progressFile == "" {
		progressFile = defaultProgressFile
	}
	updateLogFile := strings.TrimSpace(os.Getenv("DEVBOX_UPDATE_LOG"))
	if updateLogFile == "" {
		updateLogFile = defaultUpdateLog
	}
	return &Service{
		currentVersion: strings.TrimSpace(currentVersion),
		helperBinary:   strings.TrimSpace(helperBinary),
		sudoBinary:     strings.TrimSpace(sudoBinary),
		repository:     defaultRepository,
		ref:            defaultRef,
		progressFile:   progressFile,
		updateLogFile:  updateLogFile,
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
		if progress.Active() && !updateServiceActive(ctx) {
			progress.State = "failed"
			progress.Message = "Proces aktualizacji nie jest już aktywny."
			progress.Error = "Zapisany stan wskazuje trwającą aktualizację, ale devbox-update.service nie jest aktywny."
		}
		if progress.State == "failed" {
			progress.LogPath = s.updateLogFile
			if logTail, tailErr := readUpdateLogTail(s.updateLogFile, updateLogTailLines, updateLogTailBytes); tailErr == nil {
				progress.LogTail = logTail
			}
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
	var exitCode *int
	if rawExitCode := strings.TrimSpace(values["EXIT_CODE"]); rawExitCode != "" {
		if parsed, err := strconv.Atoi(rawExitCode); err == nil {
			exitCode = &parsed
		}
	}
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
		ExitCode:       exitCode,
	}, nil
}

func readUpdateLogTail(path string, maxLines, maxBytes int) ([]string, error) {
	if maxLines <= 0 || maxBytes <= 0 {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	start := info.Size() - int64(maxBytes)
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, 0); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxBytes)
	lines := make([]string, 0, maxLines)
	if start > 0 && scanner.Scan() {
		// The seek can start in the middle of a line. Discard that fragment.
	}
	for scanner.Scan() {
		line := sanitizeUpdateLogLine(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read update log tail: %w", err)
	}
	return lines, nil
}

func sanitizeUpdateLogLine(line string) string {
	line = authorizationHeaderPattern.ReplaceAllString(line, "$1[REDACTED]")
	line = sensitiveAssignmentPattern.ReplaceAllString(line, "$1$2[REDACTED]")
	line = bearerPattern.ReplaceAllString(line, "$1[REDACTED]")
	line = mysqlPasswordPattern.ReplaceAllString(line, "$1[REDACTED]")
	return strings.Map(func(r rune) rune {
		if r == '\t' || r >= 0x20 {
			return r
		}
		return -1
	}, line)
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

	// devbox-update.service is Type=oneshot. During the whole ExecStart run
	// systemd reports ActiveState=activating, not active. systemctl is-active
	// therefore returns a non-zero exit code even though the updater is still
	// executing, which used to turn valid progress into a false "failed" state.
	cmd := exec.CommandContext(
		checkCtx,
		"systemctl",
		"show",
		"--property=ActiveState",
		"--value",
		"devbox-update.service",
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return updateServiceStateRunning(string(out))
}

func updateServiceStateRunning(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "active", "activating", "reloading", "deactivating":
		return true
	default:
		return false
	}
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

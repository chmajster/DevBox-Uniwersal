package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type PHPFPMStatus struct {
	Installed   bool   `json:"installed"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	Installable bool   `json:"installable"`
	Message     string `json:"message,omitempty"`
}

type Service struct {
	helperBinary string
	sudoBinary   string
}

func NewService(helperBinary, sudoBinary string) *Service {
	return &Service{
		helperBinary: strings.TrimSpace(helperBinary),
		sudoBinary:   strings.TrimSpace(sudoBinary),
	}
}

func (s *Service) PHPFPMStatus(ctx context.Context) PHPFPMStatus {
	status := PHPFPMStatus{Installable: s.helperBinary != ""}
	path, err := findPHPFPM()
	if err != nil {
		status.Message = "PHP-FPM nie jest zainstalowany. Jest wymagany do uruchamiania aplikacji PHP w trybie PHP-FPM."
		if !status.Installable {
			status.Message += " Instalacja z panelu jest niedostępna, ponieważ DEVBOX_PRIVILEGED_HELPER nie jest skonfigurowany."
		}
		return status
	}
	status.Installed = true
	status.Path = path
	out, versionErr := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if versionErr == nil {
		line := strings.TrimSpace(string(out))
		if first, _, ok := strings.Cut(line, "\n"); ok {
			line = strings.TrimSpace(first)
		}
		status.Version = line
	}
	status.Message = "PHP-FPM jest zainstalowany i gotowy do użycia."
	return status
}

func (s *Service) InstallPHPFPM(ctx context.Context) (PHPFPMStatus, error) {
	if status := s.PHPFPMStatus(ctx); status.Installed {
		return status, nil
	}
	if s.helperBinary == "" {
		return PHPFPMStatus{}, errors.New("privileged helper is not configured")
	}
	sudo := s.sudoBinary
	if sudo == "" {
		sudo = "sudo"
	}
	cmd := exec.CommandContext(ctx, sudo, s.helperBinary, "install-package", "php-fpm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return PHPFPMStatus{}, fmt.Errorf("install PHP-FPM: %s", message)
	}
	status := s.PHPFPMStatus(ctx)
	if !status.Installed {
		return status, errors.New("PHP-FPM installation completed but no executable was detected")
	}
	return status, nil
}

func findPHPFPM() (string, error) {
	candidates := []string{"php-fpm", "php-fpm8.6", "php-fpm8.5", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1", "php-fpm8.0"}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	var discovered []string
	for _, pattern := range []string{
		"/usr/local/sbin/php-fpm*",
		"/usr/sbin/php-fpm*",
		"/usr/local/bin/php-fpm*",
		"/usr/bin/php-fpm*",
	} {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			info, err := os.Stat(match)
			if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				discovered = append(discovered, match)
			}
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(discovered)))
	if len(discovered) > 0 {
		return discovered[0], nil
	}
	return "", errors.New("php-fpm executable not found")
}

type Module struct {
	service *Service
	audit   *audit.Service
}

func NewModule(service *Service, auditService *audit.Service) *Module {
	return &Module{service: service, audit: auditService}
}

func (m *Module) Name() string { return "plugins" }

func (m *Module) RegisterRoutes(mux *http.ServeMux, middleware api.ModuleMiddleware) {
	viewer := func(handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleViewer, handler))
	}
	admin := func(handler http.HandlerFunc) http.Handler {
		return middleware.Authenticate(middleware.RequireRole(domain.RoleAdmin, handler))
	}
	mux.Handle("GET /api/v1/plugins/php-fpm/status", viewer(m.phpFPMStatus))
	mux.Handle("POST /api/v1/plugins/php-fpm/install", admin(m.installPHPFPM))
}

func (m *Module) phpFPMStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.PHPFPMStatus(r.Context()))
}

func (m *Module) installPHPFPM(w http.ResponseWriter, r *http.Request) {
	status, err := m.service.InstallPHPFPM(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "php_fpm_install_failed", err.Error())
		return
	}
	if m.audit != nil {
		var actor *string
		if user, ok := api.CurrentUser(r.Context()); ok {
			id := user.ID
			actor = &id
		}
		_ = m.audit.Record(r.Context(), actor, "plugin.php_fpm.install", "plugin", nil, map[string]any{"path": status.Path, "version": status.Version}, nil)
	}
	writeData(w, http.StatusOK, status)
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

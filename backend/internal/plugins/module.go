package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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

type DockerComposeStatus struct {
	Installed   bool   `json:"installed"`
	Mode        string `json:"mode,omitempty"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	Installable bool   `json:"installable"`
	Message     string `json:"message,omitempty"`
}

type PostgreSQLStatus struct {
	Installed   bool   `json:"installed"`
	Running     bool   `json:"running"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	Installable bool   `json:"installable"`
	Message     string `json:"message,omitempty"`
}

type HostDatabaseInstance struct {
	ID        string `json:"id"`
	Engine    string `json:"engine"`
	Label     string `json:"label"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version,omitempty"`
	Source    string `json:"source,omitempty"`
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

func (s *Service) DockerComposeStatus(ctx context.Context) DockerComposeStatus {
	status := DockerComposeStatus{Installable: s.helperBinary != ""}
	if dockerPath, err := exec.LookPath("docker"); err == nil {
		out, composeErr := exec.CommandContext(ctx, dockerPath, "compose", "version").CombinedOutput()
		if composeErr == nil {
			status.Installed = true
			status.Mode = "plugin"
			status.Path = dockerPath
			status.Version = firstLine(string(out))
			status.Message = "Docker Compose v2 jest dostępny jako plugin polecenia docker."
			return status
		}
	}
	if legacyPath, err := exec.LookPath("docker-compose"); err == nil {
		out, composeErr := exec.CommandContext(ctx, legacyPath, "version").CombinedOutput()
		if composeErr == nil {
			status.Installed = true
			status.Mode = "legacy"
			status.Path = legacyPath
			status.Version = firstLine(string(out))
			status.Message = "Dostępny jest kompatybilny klient docker-compose."
			return status
		}
	}
	status.Message = "Docker Compose nie jest zainstalowany. Projekty zawierające compose.yaml lub docker-compose.yml nie mogą zostać wdrożone."
	if !status.Installable {
		status.Message += " Instalacja z panelu jest niedostępna, ponieważ DEVBOX_PRIVILEGED_HELPER nie jest skonfigurowany."
	}
	return status
}

func (s *Service) InstallDockerCompose(ctx context.Context) (DockerComposeStatus, error) {
	if status := s.DockerComposeStatus(ctx); status.Installed {
		return status, nil
	}
	if s.helperBinary == "" {
		return DockerComposeStatus{}, errors.New("privileged helper is not configured")
	}
	if err := s.installSystemPackage(ctx, "docker-compose"); err != nil {
		return DockerComposeStatus{}, fmt.Errorf("install Docker Compose: %w", err)
	}
	status := s.DockerComposeStatus(ctx)
	if !status.Installed {
		return status, errors.New("Docker Compose installation completed but neither docker compose nor docker-compose is available")
	}
	return status, nil
}

func (s *Service) installSystemPackage(ctx context.Context, component string) error {
	sudo := s.sudoBinary
	if sudo == "" {
		sudo = "sudo"
	}
	cmd := exec.CommandContext(ctx, sudo, s.helperBinary, "install-package", component)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return errors.New(message)
	}
	return nil
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if line, _, ok := strings.Cut(value, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return value
}

func (s *Service) PostgreSQLStatus(ctx context.Context) PostgreSQLStatus {
	status := PostgreSQLStatus{Installable: s.helperBinary != "", Host: "127.0.0.1", Port: 5432}
	instances := detectHostPostgreSQL(ctx)
	if len(instances) == 0 {
		if psql, err := exec.LookPath("psql"); err == nil {
			status.Path = psql
			status.Message = "Wykryto klienta PostgreSQL, ale nie znaleziono działającej instalacji serwera."
		} else {
			status.Message = "PostgreSQL nie jest zainstalowany. Możesz doinstalować serwer z panelu Pluginy."
		}
		if !status.Installable {
			status.Message += " Instalacja z panelu jest niedostępna, ponieważ DEVBOX_PRIVILEGED_HELPER nie jest skonfigurowany."
		}
		return status
	}
	selected := instances[0]
	for _, instance := range instances {
		if instance.Running {
			selected = instance
			break
		}
	}
	status.Installed = true
	status.Running = selected.Running
	status.Version = selected.Version
	status.Port = selected.Port
	if psql, err := exec.LookPath("psql"); err == nil {
		status.Path = psql
	}
	if status.Running {
		status.Message = "PostgreSQL jest zainstalowany i odpowiada na 127.0.0.1:" + strconv.Itoa(status.Port) + "."
	} else {
		status.Message = "PostgreSQL jest zainstalowany, ale wybrany klaster nie odpowiada na 127.0.0.1:" + strconv.Itoa(status.Port) + "."
	}
	return status
}

func (s *Service) HostDatabases(ctx context.Context) []HostDatabaseInstance {
	instances := make([]HostDatabaseInstance, 0, 4)
	if mysql, ok := detectHostMySQL(ctx); ok {
		instances = append(instances, mysql)
	}
	instances = append(instances, detectHostPostgreSQL(ctx)...)
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Engine == instances[j].Engine {
			return instances[i].Port < instances[j].Port
		}
		return instances[i].Engine < instances[j].Engine
	})
	return instances
}

func detectHostMySQL(ctx context.Context) (HostDatabaseInstance, bool) {
	path, err := findMySQLServer()
	if err != nil {
		return HostDatabaseInstance{}, false
	}
	version := ""
	if out, versionErr := exec.CommandContext(ctx, path, "--version").CombinedOutput(); versionErr == nil {
		version = firstLine(string(out))
	}
	engine := "mysql"
	label := "MySQL"
	lower := strings.ToLower(path + " " + version)
	if strings.Contains(lower, "mariadb") {
		engine = "mariadb"
		label = "MariaDB"
	}
	const port = 3306
	return HostDatabaseInstance{
		ID:        engine + ":host:" + strconv.Itoa(port),
		Engine:    engine,
		Label:     label,
		Host:      "host.docker.internal",
		Port:      port,
		Installed: true,
		Running:   hostTCPPortOpen(port),
		Version:   version,
		Source:    path,
	}, true
}

func detectHostPostgreSQL(ctx context.Context) []HostDatabaseInstance {
	psql, err := exec.LookPath("psql")
	if err != nil {
		return nil
	}
	if _, serverErr := findPostgreSQLServer(); serverErr != nil {
		return nil
	}
	version := ""
	if out, versionErr := exec.CommandContext(ctx, psql, "--version").CombinedOutput(); versionErr == nil {
		version = firstLine(string(out))
	}
	if pgClusters, clustersErr := exec.LookPath("pg_lsclusters"); clustersErr == nil {
		if out, runErr := exec.CommandContext(ctx, pgClusters, "--no-header").CombinedOutput(); runErr == nil {
			if clusters := parsePostgreSQLClusters(string(out), version); len(clusters) > 0 {
				return clusters
			}
		}
	}
	const port = 5432
	return []HostDatabaseInstance{{
		ID:        "postgresql:host:" + strconv.Itoa(port),
		Engine:    "postgresql",
		Label:     "PostgreSQL",
		Host:      "host.docker.internal",
		Port:      port,
		Installed: true,
		Running:   hostTCPPortOpen(port),
		Version:   version,
		Source:    psql,
	}}
}

func parsePostgreSQLClusters(raw, version string) []HostDatabaseInstance {
	var result []HostDatabaseInstance
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 {
			continue
		}
		port, err := strconv.Atoi(fields[2])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		clusterVersion := fields[0]
		clusterName := fields[1]
		status := strings.ToLower(fields[3])
		label := "PostgreSQL " + clusterVersion
		if clusterName != "" {
			label += " / " + clusterName
		}
		result = append(result, HostDatabaseInstance{
			ID:        "postgresql:" + clusterVersion + ":" + clusterName + ":" + strconv.Itoa(port),
			Engine:    "postgresql",
			Label:     label,
			Host:      "host.docker.internal",
			Port:      port,
			Installed: true,
			Running:   status == "online",
			Version:   version,
			Source:    "pg_lsclusters",
		})
	}
	return result
}

func findMySQLServer() (string, error) {
	for _, candidate := range []string{"mysqld", "mariadbd"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	for _, candidate := range []string{
		"/usr/sbin/mysqld",
		"/usr/sbin/mariadbd",
		"/usr/local/mysql/bin/mysqld",
		"/usr/local/sbin/mysqld",
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", errors.New("mysql server executable not found")
}

func hostTCPPortOpen(port int) bool {
	if port < 1 || port > 65535 {
		return false
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func (s *Service) InstallPostgreSQL(ctx context.Context) (PostgreSQLStatus, error) {
	if status := s.PostgreSQLStatus(ctx); status.Installed {
		return status, nil
	}
	if s.helperBinary == "" {
		return PostgreSQLStatus{}, errors.New("privileged helper is not configured")
	}
	if err := s.installSystemPackage(ctx, "postgresql"); err != nil {
		return PostgreSQLStatus{}, fmt.Errorf("install PostgreSQL: %w", err)
	}
	status := s.PostgreSQLStatus(ctx)
	if !status.Installed {
		return status, errors.New("PostgreSQL installation completed but the server executable was not detected")
	}
	return status, nil
}

func findPostgreSQLServer() (string, error) {
	if pgConfig, err := exec.LookPath("pg_config"); err == nil {
		if out, runErr := exec.Command(pgConfig, "--bindir").CombinedOutput(); runErr == nil {
			path := filepath.Join(strings.TrimSpace(string(out)), "postgres")
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return path, nil
			}
		}
	}
	var discovered []string
	for _, pattern := range []string{"/usr/lib/postgresql/*/bin/postgres", "/usr/local/pgsql/bin/postgres"} {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				discovered = append(discovered, match)
			}
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(discovered)))
	if len(discovered) > 0 {
		return discovered[0], nil
	}
	return "", errors.New("postgres server executable not found")
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
	mux.Handle("GET /api/v1/plugins/docker-compose/status", viewer(m.dockerComposeStatus))
	mux.Handle("POST /api/v1/plugins/docker-compose/install", admin(m.installDockerCompose))
	mux.Handle("GET /api/v1/plugins/php-fpm/status", viewer(m.phpFPMStatus))
	mux.Handle("POST /api/v1/plugins/php-fpm/install", admin(m.installPHPFPM))
	mux.Handle("GET /api/v1/plugins/postgresql/status", viewer(m.postgreSQLStatus))
	mux.Handle("POST /api/v1/plugins/postgresql/install", admin(m.installPostgreSQL))
	mux.Handle("GET /api/v1/plugins/databases/host", viewer(m.hostDatabases))
	mux.Handle("GET /api/v1/plugins/php/extensions", viewer(m.phpExtensions))
	mux.Handle("POST /api/v1/plugins/php/extensions/install", admin(m.installPHPExtensions))
}

func (m *Module) dockerComposeStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.DockerComposeStatus(r.Context()))
}

func (m *Module) installDockerCompose(w http.ResponseWriter, r *http.Request) {
	status, err := m.service.InstallDockerCompose(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "docker_compose_install_failed", err.Error())
		return
	}
	if m.audit != nil {
		var actor *string
		if user, ok := api.CurrentUser(r.Context()); ok {
			id := user.ID
			actor = &id
		}
		_ = m.audit.Record(r.Context(), actor, "plugin.docker_compose.install", "plugin", nil, map[string]any{"mode": status.Mode, "path": status.Path, "version": status.Version}, nil)
	}
	writeData(w, http.StatusOK, status)
}

func (m *Module) hostDatabases(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.HostDatabases(r.Context()))
}

func (m *Module) postgreSQLStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.PostgreSQLStatus(r.Context()))
}

func (m *Module) installPostgreSQL(w http.ResponseWriter, r *http.Request) {
	status, err := m.service.InstallPostgreSQL(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "postgresql_install_failed", err.Error())
		return
	}
	if m.audit != nil {
		var actor *string
		if user, ok := api.CurrentUser(r.Context()); ok {
			id := user.ID
			actor = &id
		}
		_ = m.audit.Record(r.Context(), actor, "plugin.postgresql.install", "plugin", nil, map[string]any{"path": status.Path, "version": status.Version, "running": status.Running}, nil)
	}
	writeData(w, http.StatusOK, status)
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

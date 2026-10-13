package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/api"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type DockerComposeStatus struct {
	Installed   bool   `json:"installed"`
	Mode        string `json:"mode,omitempty"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	Installable bool   `json:"installable"`
	Message     string `json:"message,omitempty"`
}

type MySQLPluginStatus struct {
	Installed     bool   `json:"installed"`
	Running       bool   `json:"running"`
	Engine        string `json:"engine,omitempty"`
	ClientPath    string `json:"client_path,omitempty"`
	ServerPath    string `json:"server_path,omitempty"`
	Version       string `json:"version,omitempty"`
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"`
	ContainerHost string `json:"container_host,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	Image         string `json:"image,omitempty"`
	Volume        string `json:"volume,omitempty"`
	Network       string `json:"network,omitempty"`
	Installable   bool   `json:"installable"`
	Message       string `json:"message,omitempty"`
}

type PostgreSQLStatus struct {
	Installed     bool   `json:"installed"`
	Running       bool   `json:"running"`
	Path          string `json:"path,omitempty"`
	Version       string `json:"version,omitempty"`
	Host          string `json:"host,omitempty"`
	Port          int    `json:"port,omitempty"`
	ContainerHost string `json:"container_host,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	Image         string `json:"image,omitempty"`
	Volume        string `json:"volume,omitempty"`
	Network       string `json:"network,omitempty"`
	Installable   bool   `json:"installable"`
	Message       string `json:"message,omitempty"`
}

type dockerDatabaseServer interface {
	Action(context.Context, string) error
	ContainerState(context.Context) (bool, bool, error)
	ApplicationEndpoint() providers.DatabaseEndpoint
	Network() string
	ContainerName() string
	Image() string
	Volume() string
}

type Service struct {
	helperBinary     string
	sudoBinary       string
	jobs             jobs.JobRunner
	mysqlServer      dockerDatabaseServer
	postgresqlServer dockerDatabaseServer
}

type ServiceOption func(*Service)

func WithJobRunner(runner jobs.JobRunner) ServiceOption {
	return func(service *Service) {
		service.jobs = runner
	}
}

func WithMySQLDatabaseServer(server dockerDatabaseServer) ServiceOption {
	return func(service *Service) {
		service.mysqlServer = server
	}
}

func WithPostgreSQLDatabaseServer(server dockerDatabaseServer) ServiceOption {
	return func(service *Service) {
		service.postgresqlServer = server
	}
}

func NewService(helperBinary, sudoBinary string, options ...ServiceOption) *Service {
	service := &Service{
		helperBinary: strings.TrimSpace(helperBinary),
		sudoBinary:   strings.TrimSpace(sudoBinary),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
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

func (s *Service) MySQLStatus(ctx context.Context) MySQLPluginStatus {
	status := MySQLPluginStatus{Engine: "mysql", Port: 3306}
	if s.mysqlServer == nil {
		status.Message = "Dockerowy serwer MySQL/MariaDB nie jest skonfigurowany w DevBox."
		return status
	}
	endpoint := s.mysqlServer.ApplicationEndpoint()
	status.Port = endpoint.Port
	status.ContainerHost = endpoint.Host
	status.ContainerName = s.mysqlServer.ContainerName()
	status.Image = s.mysqlServer.Image()
	status.Volume = s.mysqlServer.Volume()
	status.Network = s.mysqlServer.Network()
	status.Installable = true
	installed, running, err := s.mysqlServer.ContainerState(ctx)
	if err != nil {
		status.Installable = false
		status.Message = "Docker nie jest dostępny dla serwera MySQL/MariaDB: " + err.Error()
		return status
	}
	status.Installed = installed
	status.Running = running
	if running {
		status.Message = "Kontener MySQL/MariaDB działa w Dockerze i jest dostępny dla aplikacji przez wspólną sieć " + status.Network + "."
	} else if installed {
		status.Message = "Kontener MySQL/MariaDB istnieje, ale nie działa."
	} else {
		status.Message = "MySQL/MariaDB nie jest zainstalowany. Instalacja z Pluginów utworzy trwały kontener Docker."
	}
	return status
}

func (s *Service) InstallMySQL(ctx context.Context) (MySQLPluginStatus, error) {
	if s.mysqlServer == nil {
		return MySQLPluginStatus{}, errors.New("Docker MySQL/MariaDB server manager is not configured")
	}
	if err := s.mysqlServer.Action(ctx, "install"); err != nil {
		return MySQLPluginStatus{}, fmt.Errorf("install Docker MySQL/MariaDB: %w", err)
	}
	status := s.MySQLStatus(ctx)
	if !status.Installed || !status.Running {
		return status, errors.New("MySQL/MariaDB container was created but is not running")
	}
	return status, nil
}

func (s *Service) MySQLAction(ctx context.Context, action string) (MySQLPluginStatus, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "install" {
		return s.InstallMySQL(ctx)
	}
	if s.mysqlServer == nil {
		return MySQLPluginStatus{}, errors.New("Docker MySQL/MariaDB server manager is not configured")
	}
	switch action {
	case "start", "stop", "restart", "uninstall":
	default:
		return MySQLPluginStatus{}, fmt.Errorf("unsupported MySQL/MariaDB plugin action %q", action)
	}
	if err := s.mysqlServer.Action(ctx, action); err != nil {
		return MySQLPluginStatus{}, fmt.Errorf("%s Docker MySQL/MariaDB: %w", action, err)
	}
	status := s.MySQLStatus(ctx)
	switch action {
	case "start", "restart":
		if !status.Installed || !status.Running {
			return status, errors.New("MySQL/MariaDB container is not running after lifecycle action")
		}
	case "stop":
		if status.Running {
			return status, errors.New("MySQL/MariaDB container is still running after stop")
		}
	case "uninstall":
		if status.Installed {
			return status, errors.New("MySQL/MariaDB container still exists after uninstall")
		}
	}
	return status, nil
}

func (s *Service) PostgreSQLStatus(ctx context.Context) PostgreSQLStatus {
	status := PostgreSQLStatus{Port: 5432}
	if s.postgresqlServer == nil {
		status.Message = "Dockerowy serwer PostgreSQL nie jest skonfigurowany w DevBox."
		return status
	}
	endpoint := s.postgresqlServer.ApplicationEndpoint()
	status.Port = endpoint.Port
	status.ContainerHost = endpoint.Host
	status.ContainerName = s.postgresqlServer.ContainerName()
	status.Image = s.postgresqlServer.Image()
	status.Volume = s.postgresqlServer.Volume()
	status.Network = s.postgresqlServer.Network()
	status.Installable = true
	installed, running, err := s.postgresqlServer.ContainerState(ctx)
	if err != nil {
		status.Installable = false
		status.Message = "Docker nie jest dostępny dla serwera PostgreSQL: " + err.Error()
		return status
	}
	status.Installed = installed
	status.Running = running
	if running {
		status.Message = "Kontener PostgreSQL działa w Dockerze i jest dostępny dla aplikacji przez wspólną sieć " + status.Network + "."
	} else if installed {
		status.Message = "Kontener PostgreSQL istnieje, ale nie działa."
	} else {
		status.Message = "PostgreSQL nie jest zainstalowany. Instalacja z Pluginów utworzy trwały kontener Docker."
	}
	return status
}

func (s *Service) InstallPostgreSQL(ctx context.Context) (PostgreSQLStatus, error) {
	if s.postgresqlServer == nil {
		return PostgreSQLStatus{}, errors.New("Docker PostgreSQL server manager is not configured")
	}
	if err := s.postgresqlServer.Action(ctx, "install"); err != nil {
		return PostgreSQLStatus{}, fmt.Errorf("install Docker PostgreSQL: %w", err)
	}
	status := s.PostgreSQLStatus(ctx)
	if !status.Installed || !status.Running {
		return status, errors.New("PostgreSQL container was created but is not running")
	}
	return status, nil
}

func (s *Service) PostgreSQLAction(ctx context.Context, action string) (PostgreSQLStatus, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "install" {
		return s.InstallPostgreSQL(ctx)
	}
	if s.postgresqlServer == nil {
		return PostgreSQLStatus{}, errors.New("Docker PostgreSQL server manager is not configured")
	}
	switch action {
	case "start", "stop", "restart", "uninstall":
	default:
		return PostgreSQLStatus{}, fmt.Errorf("unsupported PostgreSQL plugin action %q", action)
	}
	if err := s.postgresqlServer.Action(ctx, action); err != nil {
		return PostgreSQLStatus{}, fmt.Errorf("%s Docker PostgreSQL: %w", action, err)
	}
	status := s.PostgreSQLStatus(ctx)
	switch action {
	case "start", "restart":
		if !status.Installed || !status.Running {
			return status, errors.New("PostgreSQL container is not running after lifecycle action")
		}
	case "stop":
		if status.Running {
			return status, errors.New("PostgreSQL container is still running after stop")
		}
	case "uninstall":
		if status.Installed {
			return status, errors.New("PostgreSQL container still exists after uninstall")
		}
	}
	return status, nil
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
	mux.Handle("GET /api/v1/plugins/mysql/status", viewer(m.mySQLStatus))
	mux.Handle("POST /api/v1/plugins/mysql/install", admin(m.installMySQL))
	mux.Handle("POST /api/v1/plugins/mysql/{action}", admin(m.mySQLAction))
	mux.Handle("GET /api/v1/plugins/postgresql/status", viewer(m.postgreSQLStatus))
	mux.Handle("POST /api/v1/plugins/postgresql/install", admin(m.installPostgreSQL))
	mux.Handle("POST /api/v1/plugins/postgresql/{action}", admin(m.postgreSQLAction))
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

func (m *Module) mySQLStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.MySQLStatus(r.Context()))
}

func (m *Module) installMySQL(w http.ResponseWriter, r *http.Request) {
	var actor *string
	if user, ok := api.CurrentUser(r.Context()); ok {
		id := user.ID
		actor = &id
	}
	job, err := m.service.QueueMySQLInstall(r.Context(), actor)
	if err != nil {
		writeError(w, http.StatusConflict, "mysql_install_unavailable", err.Error())
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "plugin.mysql.install.enqueue", "plugin", nil, map[string]any{
			"job_id":  job.ID,
			"purpose": "application_database",
		}, nil)
	}
	writeData(w, http.StatusAccepted, job)
}

func (m *Module) mySQLAction(w http.ResponseWriter, r *http.Request) {
	var actor *string
	if user, ok := api.CurrentUser(r.Context()); ok {
		id := user.ID
		actor = &id
	}
	action := strings.ToLower(strings.TrimSpace(r.PathValue("action")))
	job, err := m.service.QueueMySQLAction(r.Context(), action, actor)
	if err != nil {
		writeError(w, http.StatusConflict, "mysql_action_unavailable", err.Error())
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "plugin.mysql."+action+".enqueue", "plugin", nil, map[string]any{
			"job_id":  job.ID,
			"purpose": "application_database",
		}, nil)
	}
	writeData(w, http.StatusAccepted, job)
}

func (m *Module) postgreSQLStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.PostgreSQLStatus(r.Context()))
}

func (m *Module) installPostgreSQL(w http.ResponseWriter, r *http.Request) {
	var actor *string
	if user, ok := api.CurrentUser(r.Context()); ok {
		id := user.ID
		actor = &id
	}
	job, err := m.service.QueuePostgreSQLInstall(r.Context(), actor)
	if err != nil {
		writeError(w, http.StatusConflict, "postgresql_install_unavailable", err.Error())
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "plugin.postgresql.install.enqueue", "plugin", nil, map[string]any{
			"job_id":  job.ID,
			"purpose": "application_database",
		}, nil)
	}
	writeData(w, http.StatusAccepted, job)
}

func (m *Module) postgreSQLAction(w http.ResponseWriter, r *http.Request) {
	var actor *string
	if user, ok := api.CurrentUser(r.Context()); ok {
		id := user.ID
		actor = &id
	}
	action := strings.ToLower(strings.TrimSpace(r.PathValue("action")))
	job, err := m.service.QueuePostgreSQLAction(r.Context(), action, actor)
	if err != nil {
		writeError(w, http.StatusConflict, "postgresql_action_unavailable", err.Error())
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "plugin.postgresql."+action+".enqueue", "plugin", nil, map[string]any{
			"job_id":  job.ID,
			"purpose": "application_database",
		}, nil)
	}
	writeData(w, http.StatusAccepted, job)
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

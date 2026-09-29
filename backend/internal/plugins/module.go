package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
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

type MySQLPluginStatus struct {
	Installed        bool   `json:"installed"`
	Running          bool   `json:"running"`
	ApplicationReady bool   `json:"application_ready"`
	Engine           string `json:"engine,omitempty"`
	ClientPath       string `json:"client_path,omitempty"`
	ServerPath       string `json:"server_path,omitempty"`
	Version          string `json:"version,omitempty"`
	Host             string `json:"host,omitempty"`
	Port             int    `json:"port,omitempty"`
	SuggestedPort    int    `json:"suggested_port,omitempty"`
	ContainerHost    string `json:"container_host,omitempty"`
	Purpose          string `json:"purpose,omitempty"`
	Installable      bool   `json:"installable"`
	Message          string `json:"message,omitempty"`
}

type PostgreSQLStatus struct {
	Installed        bool   `json:"installed"`
	Running          bool   `json:"running"`
	ApplicationReady bool   `json:"application_ready"`
	Path             string `json:"path,omitempty"`
	Version          string `json:"version,omitempty"`
	Host             string `json:"host,omitempty"`
	Port             int    `json:"port,omitempty"`
	SuggestedPort    int    `json:"suggested_port,omitempty"`
	ContainerHost    string `json:"container_host,omitempty"`
	Purpose          string `json:"purpose,omitempty"`
	Installable      bool   `json:"installable"`
	Message          string `json:"message,omitempty"`
}

type HostDatabaseInstance struct {
	ID               string `json:"id"`
	Engine           string `json:"engine"`
	Label            string `json:"label"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Installed        bool   `json:"installed"`
	Running          bool   `json:"running"`
	ApplicationReady bool   `json:"application_ready"`
	Purpose          string `json:"purpose,omitempty"`
	Version          string `json:"version,omitempty"`
	Source           string `json:"source,omitempty"`
}

type Service struct {
	helperBinary      string
	sudoBinary        string
	jobs                  jobs.JobRunner
	reservedHostPorts     map[int]string
	mysqlDetector         func(context.Context) (HostDatabaseInstance, bool)
	hostMySQLControlPlane bool
}

type ServiceOption func(*Service)

func WithJobRunner(runner jobs.JobRunner) ServiceOption {
	return func(service *Service) {
		service.jobs = runner
	}
}

func WithReservedHostPort(port int, owner string) ServiceOption {
	return func(service *Service) {
		if port < 1 || port > 65535 {
			return
		}
		if service.reservedHostPorts == nil {
			service.reservedHostPorts = make(map[int]string)
		}
		service.reservedHostPorts[port] = strings.TrimSpace(owner)
	}
}

func WithHostMySQLControlPlane(enabled bool) ServiceOption {
	return func(service *Service) {
		service.hostMySQLControlPlane = enabled
	}
}

func withMySQLDetector(detector func(context.Context) (HostDatabaseInstance, bool)) ServiceOption {
	return func(service *Service) {
		service.mysqlDetector = detector
	}
}

func NewService(helperBinary, sudoBinary string, options ...ServiceOption) *Service {
	service := &Service{
		helperBinary:      strings.TrimSpace(helperBinary),
		sudoBinary:        strings.TrimSpace(sudoBinary),
		reservedHostPorts: make(map[int]string),
		mysqlDetector:     detectHostMySQL,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
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

func (s *Service) configureApplicationDatabase(ctx context.Context, engine string, port int) error {
	if s.helperBinary == "" {
		return errors.New("privileged helper is not configured")
	}
	sudo := s.sudoBinary
	if sudo == "" {
		sudo = "sudo"
	}
	cmd := exec.CommandContext(ctx, sudo, s.helperBinary, "configure-app-database", engine, strconv.Itoa(port))
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

func (s *Service) suggestedApplicationDatabasePort(start int) int {
	for port := start; port <= start+50 && port <= 65535; port++ {
		if _, reserved := s.reservedHostPorts[port]; reserved {
			continue
		}
		if !hostTCPPortOpen(port) {
			return port
		}
	}
	return start
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if line, _, ok := strings.Cut(value, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return value
}

func (s *Service) MySQLStatus(ctx context.Context) MySQLPluginStatus {
	suggestedPort := s.suggestedApplicationDatabasePort(3306)
	status := MySQLPluginStatus{
		Installable:   s.helperBinary != "",
		Host:          "127.0.0.1",
		Port:          suggestedPort,
		SuggestedPort: suggestedPort,
		ContainerHost: "host.docker.internal",
		Purpose:       "applications",
	}

	for _, client := range []string{"mysql", "mariadb"} {
		if path, err := exec.LookPath(client); err == nil {
			status.ClientPath = path
			break
		}
	}

	detector := s.mysqlDetector
	if detector == nil {
		detector = detectHostMySQL
	}
	instance, installed := detector(ctx)
	if s.hostMySQLControlPlane {
		status.Installable = false
		status.Purpose = "control-plane"
		if installed {
			status.Installed = true
			status.Running = instance.Running
			status.Engine = instance.Engine
			status.ServerPath = instance.Source
			status.Version = instance.Version
			status.Port = instance.Port
			status.SuggestedPort = instance.Port
			status.ContainerHost = instance.Host
		}
		status.ApplicationReady = false
		status.Message = "Ten hostowy MySQL/MariaDB jest używany przez legacy control-plane DevBox i nie może być przeznaczony dla aplikacji. Najpierw przełącz DevBox na managed MySQL."
		return status
	}
	if !installed {
		if status.ClientPath != "" {
			status.Message = "Wykryto klienta MySQL/MariaDB, ale serwer aplikacyjny na hoście nie jest zainstalowany."
		} else {
			status.Message = "MySQL/MariaDB dla aplikacji nie jest zainstalowany na hoście."
		}
		if status.Installable {
			status.Message += " Możesz zainstalować go jako bazę wyłącznie dla aplikacji."
		} else {
			status.Message += " Instalacja z panelu jest niedostępna, ponieważ DEVBOX_PRIVILEGED_HELPER nie jest skonfigurowany."
		}
		return status
	}

	status.Installed = true
	status.Running = instance.Running
	status.ApplicationReady = instance.ApplicationReady
	status.Engine = instance.Engine
	status.ServerPath = instance.Source
	status.Version = instance.Version
	status.Port = instance.Port
	status.SuggestedPort = instance.Port
	status.ContainerHost = instance.Host

	switch {
	case status.ApplicationReady:
		status.Message = fmt.Sprintf("Hostowy %s działa jako baza dla aplikacji pod %s:%d.", strings.ToUpper(status.Engine), status.ContainerHost, status.Port)
	case status.Running:
		status.Message = fmt.Sprintf("Hostowy %s działa na porcie %d, ale nie nasłuchuje na interfejsie dostępnym z kontenerów. Zastosuj konfigurację aplikacyjną.", strings.ToUpper(status.Engine), status.Port)
	default:
		status.Message = fmt.Sprintf("Hostowy %s jest zainstalowany, ale usługa nie odpowiada na porcie %d.", strings.ToUpper(status.Engine), status.Port)
	}
	return status
}

func (s *Service) mysqlInstallConflict(ctx context.Context, port int) string {
	if s.hostMySQLControlPlane {
		return "Hostowy MySQL/MariaDB jest używany przez control-plane DevBox i nie może zostać przekonfigurowany jako baza aplikacyjna."
	}
	if port < 1024 || port > 65535 {
		return "Nieprawidłowy port MySQL/MariaDB. Dozwolone są porty 1024-65535."
	}
	if owner, reserved := s.reservedHostPorts[port]; reserved {
		if owner == "" {
			owner = "inna usługa DevBox"
		}
		return fmt.Sprintf("Port %d jest zarezerwowany przez %s. Wybierz inny port dla hostowego MySQL/MariaDB.", port, owner)
	}
	detector := s.mysqlDetector
	if detector == nil {
		detector = detectHostMySQL
	}
	if current, installed := detector(ctx); installed && current.Port == port {
		return ""
	}
	if hostTCPPortOpen(port) {
		return fmt.Sprintf("Port %d jest już zajęty przez inny listener. Wybierz inny port dla MySQL/MariaDB.", port)
	}
	return ""
}

func (s *Service) InstallMySQL(ctx context.Context, port int) (MySQLPluginStatus, error) {
	if port == 0 {
		port = s.suggestedApplicationDatabasePort(3306)
	}
	if conflict := s.mysqlInstallConflict(ctx, port); conflict != "" {
		return MySQLPluginStatus{}, errors.New(conflict)
	}
	if s.helperBinary == "" {
		return MySQLPluginStatus{}, errors.New("privileged helper is not configured")
	}

	status := s.MySQLStatus(ctx)
	if !status.Installed {
		// Preseed the dedicated application port before package installation so
		// the system package does not try to start on a port already owned by
		// managed devbox-mysql.
		if err := s.configureApplicationDatabase(ctx, "mysql", port); err != nil {
			return MySQLPluginStatus{}, fmt.Errorf("prepare application MySQL configuration: %w", err)
		}
		if err := s.installSystemPackage(ctx, "mysql"); err != nil {
			return MySQLPluginStatus{}, fmt.Errorf("install MySQL/MariaDB: %w", err)
		}
	}
	if err := s.configureApplicationDatabase(ctx, "mysql", port); err != nil {
		return MySQLPluginStatus{}, fmt.Errorf("configure MySQL/MariaDB for applications: %w", err)
	}

	status = s.MySQLStatus(ctx)
	if !status.Installed {
		return status, errors.New("MySQL/MariaDB installation completed but the server executable was not detected")
	}
	if status.Port != port {
		return status, fmt.Errorf("MySQL/MariaDB is configured on port %d instead of requested port %d", status.Port, port)
	}
	if !status.Running {
		return status, fmt.Errorf("MySQL/MariaDB is installed but not running on TCP %d", port)
	}
	if !status.ApplicationReady {
		return status, fmt.Errorf("MySQL/MariaDB is running on TCP %d but is not reachable from application containers", port)
	}
	return status, nil
}

func (s *Service) PostgreSQLStatus(ctx context.Context) PostgreSQLStatus {
	suggestedPort := s.suggestedApplicationDatabasePort(5432)
	status := PostgreSQLStatus{
		Installable:   s.helperBinary != "",
		Host:          "127.0.0.1",
		Port:          suggestedPort,
		SuggestedPort: suggestedPort,
		ContainerHost: "host.docker.internal",
		Purpose:       "applications",
	}
	instances := detectHostPostgreSQL(ctx)
	if len(instances) == 0 {
		if psql, err := exec.LookPath("psql"); err == nil {
			status.Path = psql
			status.Message = "Wykryto klienta PostgreSQL, ale nie znaleziono serwera aplikacyjnego."
		} else {
			status.Message = "PostgreSQL dla aplikacji nie jest zainstalowany."
		}
		if status.Installable {
			status.Message += " Możesz zainstalować go z panelu Pluginy."
		} else {
			status.Message += " Instalacja z panelu jest niedostępna, ponieważ DEVBOX_PRIVILEGED_HELPER nie jest skonfigurowany."
		}
		return status
	}
	selected := instances[0]
	for _, instance := range instances {
		if instance.ApplicationReady {
			selected = instance
			break
		}
		if instance.Running && !selected.Running {
			selected = instance
		}
	}
	status.Installed = true
	status.Running = selected.Running
	status.ApplicationReady = selected.ApplicationReady
	status.Version = selected.Version
	status.Port = selected.Port
	status.SuggestedPort = selected.Port
	if psql, err := exec.LookPath("psql"); err == nil {
		status.Path = psql
	}
	switch {
	case status.ApplicationReady:
		status.Message = fmt.Sprintf("PostgreSQL działa jako baza dla aplikacji pod %s:%d.", status.ContainerHost, status.Port)
	case status.Running:
		status.Message = fmt.Sprintf("PostgreSQL działa na porcie %d, ale nie nasłuchuje na interfejsie dostępnym z kontenerów.", status.Port)
	default:
		status.Message = fmt.Sprintf("PostgreSQL jest zainstalowany, ale wybrany klaster nie odpowiada na porcie %d.", status.Port)
	}
	return status
}

func (s *Service) postgresInstallConflict(ctx context.Context, port int) string {
	if port < 1024 || port > 65535 {
		return "Nieprawidłowy port PostgreSQL. Dozwolone są porty 1024-65535."
	}
	if owner, reserved := s.reservedHostPorts[port]; reserved {
		if owner == "" {
			owner = "inna usługa DevBox"
		}
		return fmt.Sprintf("Port %d jest zarezerwowany przez %s. Wybierz inny port dla PostgreSQL.", port, owner)
	}
	for _, current := range detectHostPostgreSQL(ctx) {
		if current.Port == port {
			return ""
		}
	}
	if hostTCPPortOpen(port) {
		return fmt.Sprintf("Port %d jest już zajęty przez inny listener. Wybierz inny port dla PostgreSQL.", port)
	}
	return ""
}

func (s *Service) InstallPostgreSQL(ctx context.Context, port int) (PostgreSQLStatus, error) {
	if port == 0 {
		port = s.suggestedApplicationDatabasePort(5432)
	}
	if conflict := s.postgresInstallConflict(ctx, port); conflict != "" {
		return PostgreSQLStatus{}, errors.New(conflict)
	}
	if s.helperBinary == "" {
		return PostgreSQLStatus{}, errors.New("privileged helper is not configured")
	}
	status := s.PostgreSQLStatus(ctx)
	if !status.Installed {
		if err := s.installSystemPackage(ctx, "postgresql"); err != nil {
			return PostgreSQLStatus{}, fmt.Errorf("install PostgreSQL: %w", err)
		}
	}
	if err := s.configureApplicationDatabase(ctx, "postgresql", port); err != nil {
		return PostgreSQLStatus{}, fmt.Errorf("configure PostgreSQL for applications: %w", err)
	}
	status = s.PostgreSQLStatus(ctx)
	if !status.Installed {
		return status, errors.New("PostgreSQL installation completed but the server executable was not detected")
	}
	if status.Port != port {
		return status, fmt.Errorf("PostgreSQL is configured on port %d instead of requested port %d", status.Port, port)
	}
	if !status.Running {
		return status, fmt.Errorf("PostgreSQL is installed but not running on TCP %d", port)
	}
	if !status.ApplicationReady {
		return status, fmt.Errorf("PostgreSQL is running on TCP %d but is not reachable from application containers", port)
	}
	return status, nil
}

func (s *Service) HostDatabases(ctx context.Context) []HostDatabaseInstance {
	instances := make([]HostDatabaseInstance, 0, 4)
	detector := s.mysqlDetector
	if detector == nil {
		detector = detectHostMySQL
	}
	if !s.hostMySQLControlPlane {
		if mysql, ok := detector(ctx); ok {
			instances = append(instances, mysql)
		}
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
	port := detectMySQLPort(ctx, path)
	running := hostMySQLDaemonRunning(ctx, engine) && hostTCPPortOpen(port)
	return HostDatabaseInstance{
		ID:               engine + ":host:" + strconv.Itoa(port),
		Engine:           engine,
		Label:            label,
		Host:             "host.docker.internal",
		Port:             port,
		Installed:        true,
		Running:          running,
		ApplicationReady: running && hostPortApplicationReachable(ctx, port),
		Purpose:          "applications",
		Version:          version,
		Source:           path,
	}, true
}

func detectMySQLPort(ctx context.Context, serverPath string) int {
	const fallback = 3306
	if raw, err := os.ReadFile("/etc/mysql/conf.d/99-devbox-application.cnf"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok || strings.TrimSpace(key) != "port" {
				continue
			}
			port, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if parseErr == nil && port >= 1024 && port <= 65535 {
				return port
			}
		}
	}
	out, err := exec.CommandContext(ctx, serverPath, "--print-defaults").CombinedOutput()
	if err != nil {
		return fallback
	}
	for _, field := range strings.Fields(string(out)) {
		value, ok := strings.CutPrefix(field, "--port=")
		if !ok {
			continue
		}
		port, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr == nil && port >= 1024 && port <= 65535 {
			return port
		}
	}
	return fallback
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
				for index := range clusters {
					clusters[index].ApplicationReady = clusters[index].Running && hostPortApplicationReachable(ctx, clusters[index].Port)
					clusters[index].Purpose = "applications"
				}
				return clusters
			}
		}
	}
	const port = 5432
	running := hostTCPPortOpen(port)
	return []HostDatabaseInstance{{
		ID:               "postgresql:host:" + strconv.Itoa(port),
		Engine:           "postgresql",
		Label:            "PostgreSQL",
		Host:             "host.docker.internal",
		Port:             port,
		Installed:        true,
		Running:          running,
		ApplicationReady: running && hostPortApplicationReachable(ctx, port),
		Purpose:          "applications",
		Version:          version,
		Source:           psql,
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
			Purpose:   "applications",
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

func hostMySQLDaemonRunning(ctx context.Context, engine string) bool {
	units := []string{"mysql.service", "mariadb.service"}
	services := []string{"mysql", "mariadb"}
	if strings.EqualFold(engine, "mariadb") {
		units = []string{"mariadb.service", "mysql.service"}
		services = []string{"mariadb", "mysql"}
	}
	if systemctl, err := exec.LookPath("systemctl"); err == nil {
		for _, unit := range units {
			if exec.CommandContext(ctx, systemctl, "is-active", "--quiet", unit).Run() == nil {
				return true
			}
		}
	}
	if service, err := exec.LookPath("service"); err == nil {
		for _, name := range services {
			if exec.CommandContext(ctx, service, name, "status").Run() == nil {
				return true
			}
		}
	}
	return false
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

func hostPortApplicationReachable(ctx context.Context, port int) bool {
	ss, err := exec.LookPath("ss")
	if err != nil {
		return false
	}
	out, err := exec.CommandContext(ctx, ss, "-ltnH").CombinedOutput()
	if err != nil {
		return false
	}
	portSuffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(string(out), "\n") {
		for _, field := range strings.Fields(line) {
			if !strings.HasSuffix(field, portSuffix) {
				continue
			}
			host := strings.TrimSuffix(field, portSuffix)
			host = strings.Trim(host, "[]")
			switch strings.ToLower(host) {
			case "127.0.0.1", "::1", "localhost":
				continue
			default:
				if host != "" {
					return true
				}
			}
		}
	}
	return false
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
	mux.Handle("GET /api/v1/plugins/mysql/status", viewer(m.mySQLStatus))
	mux.Handle("POST /api/v1/plugins/mysql/install", admin(m.installMySQL))
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

func (m *Module) mySQLStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.MySQLStatus(r.Context()))
}

func (m *Module) installMySQL(w http.ResponseWriter, r *http.Request) {
	port, err := decodeApplicationDatabasePort(r, m.service.suggestedApplicationDatabasePort(3306))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_mysql_port", err.Error())
		return
	}
	var actor *string
	if user, ok := api.CurrentUser(r.Context()); ok {
		id := user.ID
		actor = &id
	}
	job, err := m.service.QueueMySQLInstall(r.Context(), actor, port)
	if err != nil {
		writeError(w, http.StatusConflict, "mysql_install_unavailable", err.Error())
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "plugin.mysql.install.enqueue", "plugin", nil, map[string]any{
			"job_id": job.ID, "port": port, "purpose": "applications",
		}, nil)
	}
	writeData(w, http.StatusAccepted, job)
}

func (m *Module) hostDatabases(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.HostDatabases(r.Context()))
}

func (m *Module) postgreSQLStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.PostgreSQLStatus(r.Context()))
}

func (m *Module) installPostgreSQL(w http.ResponseWriter, r *http.Request) {
	port, err := decodeApplicationDatabasePort(r, m.service.suggestedApplicationDatabasePort(5432))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_postgresql_port", err.Error())
		return
	}
	var actor *string
	if user, ok := api.CurrentUser(r.Context()); ok {
		id := user.ID
		actor = &id
	}
	job, err := m.service.QueuePostgreSQLInstall(r.Context(), actor, port)
	if err != nil {
		writeError(w, http.StatusConflict, "postgresql_install_unavailable", err.Error())
		return
	}
	if m.audit != nil {
		_ = m.audit.Record(r.Context(), actor, "plugin.postgresql.install.enqueue", "plugin", nil, map[string]any{
			"job_id": job.ID, "port": port, "purpose": "applications",
		}, nil)
	}
	writeData(w, http.StatusAccepted, job)
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

func decodeApplicationDatabasePort(r *http.Request, fallback int) (int, error) {
	var input struct {
		Port int `json:"port"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		return 0, errors.New("invalid JSON body")
	}
	if input.Port == 0 {
		input.Port = fallback
	}
	if input.Port < 1024 || input.Port > 65535 {
		return 0, errors.New("database port must be between 1024 and 65535")
	}
	return input.Port, nil
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

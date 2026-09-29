package plugins

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type MySQLStatus struct {
	ServerPath    string `json:"server_path,omitempty"`
	ContainerHost string `json:"container_host,omitempty"`
	Installed     bool   `json:"installed"`
	Running       bool   `json:"running"`
	Engine        string `json:"engine"`
	Path          string `json:"path,omitempty"`
	ClientPath    string `json:"client_path,omitempty"`
	Version       string `json:"version,omitempty"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Installable   bool   `json:"installable"`
	Message       string `json:"message"`
}

func (s *Service) MySQLStatus(ctx context.Context) MySQLStatus {
	status := MySQLStatus{Engine: "mysql", Host: "127.0.0.1", ContainerHost: "host.docker.internal", Port: 3306, Installable: s.helperBinary != ""}
	if client, e := exec.LookPath("mysql"); e == nil {
		status.ClientPath = client
	} else if client, e = exec.LookPath("mariadb"); e == nil {
		status.ClientPath = client
	}
	detector := s.mysqlDetector
	if detector == nil {
		detector = detectHostMySQL
	}
	if instance, ok := detector(ctx); ok {
		status.Installed = true
		status.Running = instance.Running
		status.Engine = instance.Engine
		status.Path = instance.Source
		status.ServerPath = instance.Source
		status.Version = instance.Version
		status.Port = instance.Port
		status.Message = "Serwer bazy jest zainstalowany. Dostęp z Dockera wymaga właściwego bind-address, reguł sieciowych i grantów; DevBox nie zmienia ich automatycznie."
	} else {
		status.Message = "Serwer MySQL/MariaDB nie jest zainstalowany na hoście. Baza Docker-managed pozostaje odrębną usługą."
		if conflict := s.mysqlInstallConflict(); conflict != "" {
			status.Installable = false
			status.Message = conflict
		}
	}
	return status
}
func (s *Service) InstallMySQL(ctx context.Context) (MySQLStatus, error) {
	if status := s.MySQLStatus(ctx); status.Installed {
		return status, nil
	}
	if conflict := s.mysqlInstallConflict(); conflict != "" {
		return MySQLStatus{}, fmt.Errorf("%s", conflict)
	}
	if s.helperBinary == "" {
		return MySQLStatus{}, fmt.Errorf("privileged helper is not configured")
	}
	if err := s.installSystemPackage(ctx, "mysql"); err != nil {
		return MySQLStatus{}, err
	}
	status := s.MySQLStatus(ctx)
	if !status.Installed {
		return status, fmt.Errorf("installation completed but no MySQL/MariaDB server executable was found")
	}
	if !status.Running {
		return status, fmt.Errorf("MySQL/MariaDB installed but host daemon is not accepting connections on TCP %d", status.Port)
	}
	return status, nil
}
func (m *Module) mySQLStatus(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, m.service.MySQLStatus(r.Context()))
}
func (m *Module) installMySQL(w http.ResponseWriter, r *http.Request) {
	m.enqueueInstall(w, r, "mysql", nil)
}

func parseMySQLPort(raw string) (int, bool) {
	fields := strings.Fields(strings.ReplaceAll(raw, "\x00", " "))
	port := 0
	for i, f := range fields {
		value := ""
		if strings.HasPrefix(f, "--port=") {
			value = strings.TrimPrefix(f, "--port=")
		}
		if (f == "--port" || f == "port") && i+1 < len(fields) {
			value = fields[i+1]
		}
		if p, e := strconv.Atoi(value); e == nil && p >= 1 && p <= 65535 {
			port = p
		}
	}
	return port, port != 0
}
func discoverMySQLPort(parent context.Context, server string) int {
	// Prefer explicit arguments of a host server. Container cgroups are excluded.
	procs, _ := filepath.Glob("/proc/[0-9]*/comm")
	for _, file := range procs {
		comm, e := os.ReadFile(file)
		if e != nil {
			continue
		}
		name := strings.TrimSpace(string(comm))
		if name != "mysqld" && name != "mariadbd" {
			continue
		}
		cgroup, e := os.ReadFile(filepath.Join(filepath.Dir(file), "cgroup"))
		if e != nil {
			continue
		}
		if strings.Contains(string(cgroup), "docker") || strings.Contains(string(cgroup), "kubepods") || strings.Contains(string(cgroup), "containerd") {
			continue
		}
		args, e := os.ReadFile(filepath.Join(filepath.Dir(file), "cmdline"))
		if e == nil {
			if p, ok := parseMySQLPort(string(args)); ok {
				return p
			}
		}
	}
	// --print-defaults reads included option files without starting a server.
	// Its output may contain configuration secrets: only the integer port escapes.
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	if out, e := exec.CommandContext(ctx, server, "--print-defaults").Output(); e == nil {
		if p, ok := parseMySQLPort(string(out)); ok {
			return p
		}
	}
	if defaults, e := exec.LookPath("my_print_defaults"); e == nil {
		if out, e := exec.CommandContext(ctx, defaults, "mysqld", "server", "mariadb", "mariadbd").Output(); e == nil {
			if p, ok := parseMySQLPort(string(out)); ok {
				return p
			}
		}
	}
	return 3306
}

func (s *Service) mysqlInstallConflict() string {
	if owner, reserved := s.reservedHostPorts[3306]; reserved {
		if owner == "" {
			owner = "inna usługa DevBox"
		}
		return fmt.Sprintf("Nie można zainstalować hostowego MySQL/MariaDB na porcie 3306: port jest zarezerwowany przez %s. Zatrzymaj lub przekonfiguruj tę usługę albo użyj już dostępnej bazy przez DevBox.", owner)
	}
	if hostTCPPortOpen(3306) {
		return "Nie można zainstalować hostowego MySQL/MariaDB: port 3306 jest już zajęty przez inny listener. DevBox nie uruchomi drugiego serwera na tym samym porcie."
	}
	return ""
}

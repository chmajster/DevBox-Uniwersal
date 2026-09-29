package databases

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const (
	phpMyAdminHostDatabaseHost = "host.docker.internal"
	phpMyAdminHostDatabasePort = 3306
)

type PHPMyAdminConfig struct {
	DockerBinary string
	Image        string
	Container    string
	HostPort     int
	MySQLHost    string
	MySQLPort    int
	Network      string
}

type PHPMyAdminManager struct {
	cfg PHPMyAdminConfig
}

func NewPHPMyAdminManager(cfg PHPMyAdminConfig) *PHPMyAdminManager {
	if cfg.DockerBinary == "" {
		cfg.DockerBinary = "docker"
	}
	if cfg.Image == "" {
		cfg.Image = "phpmyadmin:5.2-apache"
	}
	if cfg.Container == "" {
		cfg.Container = "devbox-phpmyadmin"
	}
	if cfg.HostPort == 0 {
		cfg.HostPort = 8081
	}
	if cfg.MySQLHost == "" {
		cfg.MySQLHost = DefaultManagedMySQLContainer
		if cfg.Network == "" {
			cfg.Network = DefaultManagedMySQLNetwork
		}
	}
	if cfg.MySQLPort == 0 {
		cfg.MySQLPort = 3306
	}
	return &PHPMyAdminManager{cfg: cfg}
}

func (m *PHPMyAdminManager) Install(ctx context.Context) (PHPMyAdminStatus, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return PHPMyAdminStatus{}, err
	}
	if status.Installed {
		matches, matchErr := m.matchesConfiguration(ctx)
		if matchErr != nil {
			return PHPMyAdminStatus{}, matchErr
		}
		if matches {
			return status, nil
		}
		if err := m.run(ctx, "rm", "-f", m.cfg.Container); err != nil {
			return PHPMyAdminStatus{}, fmt.Errorf("replace outdated phpMyAdmin container: %w", err)
		}
	}
	if err := m.run(ctx, "pull", m.cfg.Image); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("pull phpMyAdmin image: %w", err)
	}
	args := m.createArgs()
	if err := m.run(ctx, args...); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("create phpMyAdmin container: %w", err)
	}
	return m.Status(ctx)
}

func (m *PHPMyAdminManager) matchesConfiguration(ctx context.Context) (bool, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, m.cfg.DockerBinary, "inspect", m.cfg.Container)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("inspect phpMyAdmin configuration: %w", err)
	}
	var raw []struct {
		Config struct {
			Env []string `json:"Env"`
		} `json:"Config"`
		HostConfig struct {
			ExtraHosts []string `json:"ExtraHosts"`
		} `json:"HostConfig"`
		NetworkSettings struct {
			Networks map[string]json.RawMessage `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil || len(raw) != 1 {
		if err == nil {
			err = errors.New("expected one phpMyAdmin container")
		}
		return false, fmt.Errorf("decode phpMyAdmin configuration: %w", err)
	}
	actualEnvironment := make(map[string]string, len(raw[0].Config.Env))
	for _, entry := range raw[0].Config.Env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			actualEnvironment[key] = value
		}
	}
	for key, expected := range m.requiredEnvironment() {
		if actualEnvironment[key] != expected {
			return false, nil
		}
	}
	if !containsExact(raw[0].HostConfig.ExtraHosts, phpMyAdminHostDatabaseHost+":host-gateway") {
		return false, nil
	}
	if m.cfg.Network != "" {
		if _, ok := raw[0].NetworkSettings.Networks[m.cfg.Network]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func (m *PHPMyAdminManager) Reconcile(ctx context.Context) (PHPMyAdminStatus, error) {
	current, err := m.Status(ctx)
	if err != nil {
		return PHPMyAdminStatus{}, err
	}
	if !current.Installed {
		return current, nil
	}
	wasRunning := current.Running
	updated, err := m.Install(ctx)
	if err != nil {
		return PHPMyAdminStatus{}, err
	}
	if wasRunning && !updated.Running {
		return m.Start(ctx)
	}
	return updated, nil
}

func (m *PHPMyAdminManager) Start(ctx context.Context) (PHPMyAdminStatus, error) {
	status, err := m.Install(ctx)
	if err != nil {
		return PHPMyAdminStatus{}, err
	}
	if status.Running {
		return status, nil
	}
	if err := m.run(ctx, "start", m.cfg.Container); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("start phpMyAdmin: %w", err)
	}
	return m.Status(ctx)
}

func (m *PHPMyAdminManager) Stop(ctx context.Context) (PHPMyAdminStatus, error) {
	if err := m.run(ctx, "stop", m.cfg.Container); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("stop phpMyAdmin: %w", err)
	}
	return m.Status(ctx)
}

func (m *PHPMyAdminManager) Restart(ctx context.Context) (PHPMyAdminStatus, error) {
	status, err := m.Install(ctx)
	if err != nil {
		return PHPMyAdminStatus{}, err
	}
	action := "start"
	if status.Running {
		action = "restart"
	}
	if err := m.run(ctx, action, m.cfg.Container); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("%s phpMyAdmin: %w", action, err)
	}
	return m.Status(ctx)
}

func (m *PHPMyAdminManager) Uninstall(ctx context.Context) (PHPMyAdminStatus, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return PHPMyAdminStatus{}, err
	}
	if !status.Installed {
		return status, nil
	}
	if err := m.run(ctx, "rm", "-f", m.cfg.Container); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("uninstall phpMyAdmin: %w", err)
	}
	return m.Status(ctx)
}

func (m *PHPMyAdminManager) createArgs() []string {
	args := []string{"create", "--name", m.cfg.Container}
	if m.cfg.Network != "" {
		args = append(args, "--network", m.cfg.Network)
	}
	args = append(args,
		"-p", "127.0.0.1:"+strconv.Itoa(m.cfg.HostPort)+":80",
		"--add-host", phpMyAdminHostDatabaseHost+":host-gateway",
	)

	environment := m.requiredEnvironment()
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "-e", key+"="+environment[key])
	}
	args = append(args, m.cfg.Image)
	return args
}

func (m *PHPMyAdminManager) requiredEnvironment() map[string]string {
	mysqlHost, _ := dockerMySQLTarget(m.cfg.MySQLHost)
	environment := map[string]string{
		"PMA_ARBITRARY": "1",
	}

	if strings.EqualFold(mysqlHost, phpMyAdminHostDatabaseHost) && m.cfg.MySQLPort == phpMyAdminHostDatabasePort {
		environment["PMA_HOST"] = phpMyAdminHostDatabaseHost
		environment["PMA_PORT"] = strconv.Itoa(phpMyAdminHostDatabasePort)
		environment["PMA_VERBOSE"] = "MySQL/MariaDB na hoście"
		return environment
	}

	environment["PMA_HOSTS"] = mysqlHost + "," + phpMyAdminHostDatabaseHost
	environment["PMA_PORTS"] = strconv.Itoa(m.cfg.MySQLPort) + "," + strconv.Itoa(phpMyAdminHostDatabasePort)
	environment["PMA_VERBOSES"] = "DevBox MySQL,MySQL/MariaDB na hoście"
	return environment
}

func containsExact(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func (m *PHPMyAdminManager) Status(ctx context.Context) (PHPMyAdminStatus, error) {
	url := "http://127.0.0.1:" + strconv.Itoa(m.cfg.HostPort)
	if err := m.run(ctx, "version", "--format", "{{.Server.Version}}"); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("docker is unavailable: %w", err)
	}
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, m.cfg.DockerBinary, "inspect", "--format", "{{.State.Status}}", m.cfg.Container)
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return PHPMyAdminStatus{
				Installed: false, Running: false, State: "not_installed", URL: url,
				HostDatabaseAccess: false, HostDatabaseHost: phpMyAdminHostDatabaseHost, HostDatabasePort: phpMyAdminHostDatabasePort,
			}, nil
		}
		return PHPMyAdminStatus{}, fmt.Errorf("inspect phpMyAdmin container: %w", err)
	}
	state := strings.TrimSpace(stdout.String())
	hostAccess, _ := m.matchesConfiguration(ctx)
	hostReachable := false
	if state == "running" && hostAccess {
		hostReachable = m.hostDatabaseReachable(ctx)
	}
	return PHPMyAdminStatus{
		Installed: true, Running: state == "running", State: state, URL: url,
		HostDatabaseAccess: hostAccess, HostDatabaseReachable: hostReachable,
		HostDatabaseHost: phpMyAdminHostDatabaseHost, HostDatabasePort: phpMyAdminHostDatabasePort,
	}, nil
}

func (m *PHPMyAdminManager) hostDatabaseReachable(ctx context.Context) bool {
	script := fmt.Sprintf(
		`$s=@fsockopen("%s",%d,$errno,$errstr,2); if ($s) { fclose($s); exit(0); } exit(1);`,
		phpMyAdminHostDatabaseHost,
		phpMyAdminHostDatabasePort,
	)
	cmd := exec.CommandContext(ctx, m.cfg.DockerBinary, "exec", m.cfg.Container, "php", "-r", script)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func (m *PHPMyAdminManager) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, m.cfg.DockerBinary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker command failed: %w", err)
	}
	return nil
}

func dockerMySQLTarget(host string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "localhost", "::1":
		return "host.docker.internal", true
	default:
		return host, false
	}
}

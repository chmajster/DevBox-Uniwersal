package databases

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const phpMyAdminManagedPasswordPath = "/tmp/devbox-managed-mysql-password"

type PHPMyAdminConfig struct {
	DockerBinary string
	Image        string
	Container    string
	HostPort     int
	MySQLHost    string
	MySQLPort    int
	Network      string
	ManagedMySQL *ManagedMySQLManager
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

func (m *PHPMyAdminManager) databaseTarget() (string, int, bool) {
	host, hostGateway := dockerMySQLTarget(m.cfg.MySQLHost)
	return host, m.cfg.MySQLPort, hostGateway
}

func (m *PHPMyAdminManager) resolvedDatabaseTarget(ctx context.Context) (string, int, bool, bool, error) {
	host, port, hostGateway := m.databaseTarget()
	if m.cfg.ManagedMySQL == nil {
		return host, port, hostGateway, false, nil
	}
	installed, _, err := m.cfg.ManagedMySQL.ContainerState(ctx)
	if err != nil {
		return "", 0, false, false, fmt.Errorf("detect managed MySQL for phpMyAdmin: %w", err)
	}
	if !installed {
		return host, port, hostGateway, false, nil
	}
	endpoint := m.cfg.ManagedMySQL.ApplicationEndpoint()
	return endpoint.Host, endpoint.Port, false, true, nil
}

func (m *PHPMyAdminManager) Install(ctx context.Context) (PHPMyAdminStatus, error) {
	status, err := m.Status(ctx)
	if err != nil {
		return PHPMyAdminStatus{}, err
	}
	environment, err := m.requiredEnvironment(ctx)
	if err != nil {
		return PHPMyAdminStatus{}, err
	}
	if status.Installed {
		matches, matchErr := m.matchesConfiguration(ctx, environment)
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
	if err := m.run(ctx, m.createArgs(environment)...); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("create phpMyAdmin container: %w", err)
	}
	if err := m.installManagedCredential(ctx, environment); err != nil {
		_ = m.run(ctx, "rm", "-f", m.cfg.Container)
		return PHPMyAdminStatus{}, err
	}
	return m.Status(ctx)
}

func (m *PHPMyAdminManager) matchesConfiguration(ctx context.Context, expectedEnvironment map[string]string) (bool, error) {
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
	for key, expected := range expectedEnvironment {
		if actualEnvironment[key] != expected {
			return false, nil
		}
	}
	for _, key := range []string{"PMA_USER", "PMA_PASSWORD", "PMA_PASSWORD_FILE"} {
		if _, expected := expectedEnvironment[key]; !expected {
			if _, present := actualEnvironment[key]; present {
				return false, nil
			}
		}
	}
	hostGateway := expectedEnvironment["PMA_HOST"] == "host.docker.internal"
	if hostGateway && !containsExact(raw[0].HostConfig.ExtraHosts, "host.docker.internal:host-gateway") {
		return false, nil
	}
	if !hostGateway && containsExact(raw[0].HostConfig.ExtraHosts, "host.docker.internal:host-gateway") {
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

func (m *PHPMyAdminManager) createArgs(environment map[string]string) []string {
	args := []string{"create", "--name", m.cfg.Container}
	if m.cfg.Network != "" {
		args = append(args, "--network", m.cfg.Network)
	}
	args = append(args, "-p", "127.0.0.1:"+strconv.Itoa(m.cfg.HostPort)+":80")

	if environment["PMA_HOST"] == "host.docker.internal" {
		args = append(args, "--add-host", "host.docker.internal:host-gateway")
	}

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

func (m *PHPMyAdminManager) requiredEnvironment(ctx context.Context) (map[string]string, error) {
	host, port, _, managed, err := m.resolvedDatabaseTarget(ctx)
	if err != nil {
		return nil, err
	}
	environment := map[string]string{
		"PMA_ARBITRARY": "1",
		"PMA_HOST":      host,
		"PMA_PORT":      strconv.Itoa(port),
		"PMA_VERBOSE":   "DevBox MySQL/MariaDB",
	}
	if !managed {
		return environment, nil
	}
	environment["PMA_ARBITRARY"] = "0"
	environment["PMA_USER"] = "root"
	environment["PMA_PASSWORD_FILE"] = phpMyAdminManagedPasswordPath
	return environment, nil
}

func (m *PHPMyAdminManager) installManagedCredential(ctx context.Context, environment map[string]string) error {
	passwordPath := strings.TrimSpace(environment["PMA_PASSWORD_FILE"])
	if passwordPath == "" || m.cfg.ManagedMySQL == nil {
		return nil
	}
	password, err := m.cfg.ManagedMySQL.RootPassword(ctx)
	if err != nil {
		return fmt.Errorf("load managed MySQL credential for phpMyAdmin: %w", err)
	}
	defer clear(password)

	tmp, err := os.CreateTemp("", "devbox-phpmyadmin-mysql-password-*")
	if err != nil {
		return fmt.Errorf("create temporary phpMyAdmin credential file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure temporary phpMyAdmin credential file: %w", err)
	}
	if _, err := tmp.Write(password); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary phpMyAdmin credential file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary phpMyAdmin credential file: %w", err)
	}
	if err := m.run(ctx, "cp", tmpName, m.cfg.Container+":"+passwordPath); err != nil {
		return fmt.Errorf("copy managed MySQL credential into phpMyAdmin container: %w", err)
	}
	return nil
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
	host, port, _, _, targetErr := m.resolvedDatabaseTarget(ctx)
	if targetErr != nil {
		return PHPMyAdminStatus{}, targetErr
	}
	base := PHPMyAdminStatus{
		URL:          url,
		DatabaseHost: host,
		DatabasePort: port,
		Network:      m.cfg.Network,
	}
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
			base.State = "not_installed"
			return base, nil
		}
		return PHPMyAdminStatus{}, fmt.Errorf("inspect phpMyAdmin container: %w", err)
	}
	base.Installed = true
	base.State = strings.TrimSpace(stdout.String())
	base.Running = base.State == "running"
	return base, nil
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

package databases

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
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
	mysqlHost, addHostGateway := dockerMySQLTarget(m.cfg.MySQLHost)
	args := []string{
		"create",
		"--name", m.cfg.Container,
	}
	if m.cfg.Network != "" {
		args = append(args, "--network", m.cfg.Network)
	}
	args = append(args, "-p", "127.0.0.1:"+strconv.Itoa(m.cfg.HostPort)+":80")
	if addHostGateway {
		args = append(args, "--add-host", "host.docker.internal:host-gateway")
	}
	args = append(args,
		"-e", "PMA_HOST="+mysqlHost,
		"-e", "PMA_PORT="+strconv.Itoa(m.cfg.MySQLPort),
		m.cfg.Image,
	)
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
	mysqlHost, _ := dockerMySQLTarget(m.cfg.MySQLHost)
	expectedHost := "PMA_HOST=" + mysqlHost
	expectedPort := "PMA_PORT=" + strconv.Itoa(m.cfg.MySQLPort)
	hasHost, hasPort := false, false
	for _, entry := range raw[0].Config.Env {
		switch entry {
		case expectedHost:
			hasHost = true
		case expectedPort:
			hasPort = true
		}
	}
	if !hasHost || !hasPort {
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
	if err := m.run(ctx, "restart", m.cfg.Container); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("restart phpMyAdmin: %w", err)
	}
	return m.Status(ctx)
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
			return PHPMyAdminStatus{Installed: false, Running: false, State: "not_installed", URL: url}, nil
		}
		return PHPMyAdminStatus{}, fmt.Errorf("inspect phpMyAdmin container: %w", err)
	}
	state := strings.TrimSpace(stdout.String())
	return PHPMyAdminStatus{Installed: true, Running: state == "running", State: state, URL: url}, nil
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

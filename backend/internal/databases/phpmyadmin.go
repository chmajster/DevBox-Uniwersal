package databases

import (
	"bytes"
	"context"
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
		cfg.MySQLHost = "127.0.0.1"
	}
	if cfg.MySQLPort == 0 {
		cfg.MySQLPort = 3306
	}
	return &PHPMyAdminManager{cfg: cfg}
}

func (m *PHPMyAdminManager) Install(ctx context.Context) (PHPMyAdminStatus, error) {
	status, err := m.Status(ctx)
	if err == nil && status.Installed {
		return status, nil
	}
	if err := m.run(ctx, "pull", m.cfg.Image); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("pull phpMyAdmin image: %w", err)
	}
	args := []string{
		"create",
		"--name", m.cfg.Container,
		"-p", "127.0.0.1:" + strconv.Itoa(m.cfg.HostPort) + ":80",
		"-e", "PMA_HOST=" + m.cfg.MySQLHost,
		"-e", "PMA_PORT=" + strconv.Itoa(m.cfg.MySQLPort),
		m.cfg.Image,
	}
	if err := m.run(ctx, args...); err != nil {
		return PHPMyAdminStatus{}, fmt.Errorf("create phpMyAdmin container: %w", err)
	}
	return m.Status(ctx)
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

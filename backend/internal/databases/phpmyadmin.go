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
	"time"
)

type PHPMyAdminConfig struct {
	HostDatabasePort int
	DockerBinary     string
	Image            string
	Container        string
	HostPort         int
	MySQLHost        string
	MySQLPort        int
	Network          string
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
	if cfg.HostDatabasePort == 0 {
		cfg.HostDatabasePort = 3306
	}
	return &PHPMyAdminManager{cfg: cfg}
}

func (m *PHPMyAdminManager) Install(ctx context.Context) (status PHPMyAdminStatus, err error) {
	status, err = m.Status(ctx)
	if err != nil {
		return status, err
	}
	if status.Installed {
		matches, e := m.matchesConfiguration(ctx)
		if e != nil {
			return status, e
		}
		if matches {
			return status, nil
		}
	}
	// Fetch before touching the current working container.
	if err = m.run(ctx, "pull", m.cfg.Image); err != nil {
		return status, fmt.Errorf("pull phpMyAdmin image: %w", err)
	}
	previous := m.cfg.Container + "-previous"
	hadPrevious, wasRunning := status.Installed, status.Running
	if hadPrevious {
		if err = m.run(ctx, "rename", m.cfg.Container, previous); err != nil {
			return status, fmt.Errorf("preserve phpMyAdmin before reconfiguration: %w", err)
		}
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = m.run(cleanup, "rm", "-f", m.cfg.Container)
		if hadPrevious {
			_ = m.run(cleanup, "rename", previous, m.cfg.Container)
			if wasRunning {
				_ = m.run(cleanup, "start", m.cfg.Container)
			}
		}
	}()
	if wasRunning {
		if err = m.run(ctx, "stop", "--time", "10", previous); err != nil {
			return status, err
		}
	}
	if err = m.run(ctx, m.createArgs()...); err != nil {
		return status, fmt.Errorf("create phpMyAdmin container: %w", err)
	}
	if wasRunning {
		if err = m.run(ctx, "start", m.cfg.Container); err != nil {
			return status, err
		}
	}
	status, err = m.Status(ctx)
	if err != nil {
		return status, err
	}
	if !status.Installed || !status.HostDatabaseAccess {
		return status, fmt.Errorf("phpMyAdmin configuration verification failed")
	}
	completed = true
	if hadPrevious {
		_ = m.run(ctx, "rm", "-f", previous)
	}
	return status, nil
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
		HostConfig struct {
			ExtraHosts   []string `json:"ExtraHosts"`
			PortBindings map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			} `json:"PortBindings"`
		} `json:"HostConfig"`
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
	actual := map[string]string{}
	for _, entry := range raw[0].Config.Env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			actual[key] = value
		}
	}
	for key, value := range m.requiredEnvironment() {
		if actual[key] != value {
			return false, nil
		}
	}
	gateway := false
	for _, host := range raw[0].HostConfig.ExtraHosts {
		if host == "host.docker.internal:host-gateway" {
			gateway = true
		}
	}
	if !gateway {
		return false, nil
	}
	bindings := raw[0].HostConfig.PortBindings["80/tcp"]
	if len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" || bindings[0].HostPort != strconv.Itoa(m.cfg.HostPort) {
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
	if !status.Running {
		return m.Start(ctx)
	}
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
	hostAccess, _ := m.matchesConfiguration(ctx)
	reachable := false
	if state == "running" && hostAccess {
		reachable = m.hostDatabaseReachable(ctx)
	}
	return PHPMyAdminStatus{Installed: true, Running: state == "running", State: state, URL: url,
		HostDatabaseAccess: hostAccess, HostDatabaseReachable: reachable, HostDatabaseHost: "host.docker.internal", HostDatabasePort: m.cfg.HostDatabasePort}, nil
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

func (m *PHPMyAdminManager) createArgs() []string {
	args := []string{"create", "--name", m.cfg.Container, "--restart", "unless-stopped", "--security-opt", "no-new-privileges:true"}
	if m.cfg.Network != "" {
		args = append(args, "--network", m.cfg.Network)
	}
	args = append(args, "-p", "127.0.0.1:"+strconv.Itoa(m.cfg.HostPort)+":80", "--add-host", "host.docker.internal:host-gateway")
	env := m.requiredEnvironment()
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+env[k])
	}
	return append(args, m.cfg.Image)
}
func (m *PHPMyAdminManager) requiredEnvironment() map[string]string {
	host, _ := dockerMySQLTarget(m.cfg.MySQLHost)
	env := map[string]string{"PMA_ARBITRARY": "1"}
	if host == "host.docker.internal" && m.cfg.MySQLPort == m.cfg.HostDatabasePort {
		env["PMA_HOST"] = host
		env["PMA_PORT"] = strconv.Itoa(m.cfg.MySQLPort)
		env["PMA_VERBOSE"] = "MySQL/MariaDB na hoście"
	} else {
		env["PMA_HOSTS"] = host + ",host.docker.internal"
		env["PMA_PORTS"] = strconv.Itoa(m.cfg.MySQLPort) + "," + strconv.Itoa(m.cfg.HostDatabasePort)
		env["PMA_VERBOSES"] = "DevBox MySQL,MySQL/MariaDB na hoście"
	}
	return env
}
func (m *PHPMyAdminManager) hostDatabaseReachable(parent context.Context) bool {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	script := `$s=@fsockopen("host.docker.internal",(int)$argv[1],$errno,$errstr,2);if($s){fclose($s);exit(0);}exit(1);`
	return exec.CommandContext(ctx, m.cfg.DockerBinary, "exec", m.cfg.Container, "php", "-r", script, "--", strconv.Itoa(m.cfg.HostDatabasePort)).Run() == nil
}

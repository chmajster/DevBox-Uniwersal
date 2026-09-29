package databases

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

const (
	DefaultManagedMySQLContainer = "devbox-mysql"
	DefaultManagedMySQLNetwork   = "devbox-apps"
	DefaultManagedMySQLVolume    = "devbox-mysql-data"
	DefaultManagedMySQLImage     = "mysql:8.4"
	DefaultManagedMySQLAdminPort = 13306

	managedMySQLSecretScope = "managed-mysql"
	managedMySQLSecretName  = "root-password"
)

type ManagedMySQLConfig struct {
	Image               string
	Container           string
	Network             string
	Volume              string
	AdminHost           string
	AdminPort           int
	InitialRootPassword string
}

type ManagedMySQLManager struct {
	docker  providers.DockerInfrastructureProvider
	secrets secrets.SecretStore
	cfg     ManagedMySQLConfig
}

func NewManagedMySQLManager(dockerProvider providers.DockerInfrastructureProvider, secretStore secrets.SecretStore, cfg ManagedMySQLConfig) *ManagedMySQLManager {
	if cfg.Image == "" {
		cfg.Image = DefaultManagedMySQLImage
	}
	if cfg.Container == "" {
		cfg.Container = DefaultManagedMySQLContainer
	}
	if cfg.Network == "" {
		cfg.Network = DefaultManagedMySQLNetwork
	}
	if cfg.Volume == "" {
		cfg.Volume = DefaultManagedMySQLVolume
	}
	if cfg.AdminHost == "" {
		cfg.AdminHost = "127.0.0.1"
	}
	if cfg.AdminPort == 0 {
		cfg.AdminPort = DefaultManagedMySQLAdminPort
	}
	return &ManagedMySQLManager{docker: dockerProvider, secrets: secretStore, cfg: cfg}
}

func (m *ManagedMySQLManager) Ensure(ctx context.Context) error {
	if m == nil || m.docker == nil {
		return errors.New("managed MySQL Docker provider is not configured")
	}
	if m.secrets == nil {
		return ErrSecretsUnavailable
	}
	if err := m.docker.EnsureNetwork(ctx, m.cfg.Network); err != nil {
		return fmt.Errorf("ensure managed MySQL network: %w", err)
	}
	if err := m.docker.EnsureVolume(ctx, m.cfg.Volume); err != nil {
		return fmt.Errorf("ensure managed MySQL volume: %w", err)
	}
	if err := m.docker.EnsureImage(ctx, m.cfg.Image); err != nil {
		return fmt.Errorf("ensure managed MySQL image: %w", err)
	}
	password, err := m.RootPassword(ctx)
	if err != nil {
		return err
	}
	defer clear(password)
	spec := providers.ContainerSpec{
		Name:          m.cfg.Container,
		Image:         m.cfg.Image,
		RestartPolicy: "unless-stopped",
		Networks:      []string{m.cfg.Network},
		Volumes:       map[string]string{m.cfg.Volume: "/var/lib/mysql"},
		PortBindings: []providers.ContainerPortBinding{{
			HostIP: "127.0.0.1", HostPort: m.cfg.AdminPort, ContainerPort: 3306,
		}},
		SensitiveEnvironment: map[string]string{
			"MYSQL_ROOT_PASSWORD": string(password),
			"MYSQL_ROOT_HOST":     "%",
		},
		Labels: map[string]string{
			"io.devbox.managed-mysql":            "true",
			"io.devbox.managed-mysql.admin-port": strconv.Itoa(m.cfg.AdminPort),
		},
	}
	item, err := m.docker.EnsureContainer(ctx, spec)
	if err != nil {
		return fmt.Errorf("ensure managed MySQL container: %w", err)
	}
	if err := m.docker.ConnectNetwork(ctx, m.cfg.Container, m.cfg.Network); err != nil {
		return fmt.Errorf("reconcile managed MySQL network: %w", err)
	}
	if !strings.EqualFold(item.State, "running") {
		if err := m.docker.Start(ctx, m.cfg.Container); err != nil {
			return fmt.Errorf("start managed MySQL: %w", err)
		}
	}
	return nil
}

func (m *ManagedMySQLManager) RootPassword(ctx context.Context) ([]byte, error) {
	if m == nil || m.secrets == nil {
		return nil, ErrSecretsUnavailable
	}
	value, err := m.secrets.Get(ctx, managedMySQLSecretScope, managedMySQLSecretName)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, secrets.ErrNotFound) {
		return nil, fmt.Errorf("load managed MySQL root credential: %w", err)
	}
	password := strings.TrimSpace(m.cfg.InitialRootPassword)
	if password == "" {
		password, err = GeneratePassword()
		if err != nil {
			return nil, err
		}
	}
	if err := m.secrets.Put(ctx, managedMySQLSecretScope, managedMySQLSecretName, []byte(password)); err != nil {
		return nil, fmt.Errorf("store managed MySQL root credential: %w", err)
	}
	return []byte(password), nil
}

func (m *ManagedMySQLManager) AdminSecretRef() (scope, name string) {
	return managedMySQLSecretScope, managedMySQLSecretName
}

func (m *ManagedMySQLManager) AdminEndpoint() providers.DatabaseEndpoint {
	return providers.DatabaseEndpoint{Host: m.cfg.AdminHost, Port: m.cfg.AdminPort}
}

func (m *ManagedMySQLManager) ApplicationEndpoint() providers.DatabaseEndpoint {
	return providers.DatabaseEndpoint{Host: m.cfg.Container, Port: 3306}
}

func (m *ManagedMySQLManager) Network() string {
	return m.cfg.Network
}

func (m *ManagedMySQLManager) ContainerName() string {
	return m.cfg.Container
}

func (m *ManagedMySQLManager) Image() string {
	return m.cfg.Image
}

func (m *ManagedMySQLManager) Volume() string {
	return m.cfg.Volume
}

func (m *ManagedMySQLManager) ContainerState(ctx context.Context) (installed bool, running bool, err error) {
	if m == nil || m.docker == nil {
		return false, false, errors.New("managed MySQL Docker provider is not configured")
	}
	if err := m.docker.Available(ctx); err != nil {
		return false, false, err
	}
	item, err := m.docker.Inspect(ctx, m.cfg.Container)
	if err != nil {
		return false, false, nil
	}
	return true, strings.EqualFold(item.State, "running"), nil
}

func (m *ManagedMySQLManager) Action(ctx context.Context, action string) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "install", "start":
		return m.Ensure(ctx)
	case "stop":
		return m.docker.Stop(ctx, m.cfg.Container)
	case "restart":
		if err := m.Ensure(ctx); err != nil {
			return err
		}
		return m.docker.Restart(ctx, m.cfg.Container)
	case "uninstall":
		installed, running, err := m.ContainerState(ctx)
		if err != nil {
			return err
		}
		if !installed {
			return nil
		}
		if running {
			if err := m.docker.Stop(ctx, m.cfg.Container); err != nil {
				return fmt.Errorf("stop managed MySQL before uninstall: %w", err)
			}
		}
		if err := m.docker.Remove(ctx, m.cfg.Container); err != nil {
			return fmt.Errorf("remove managed MySQL container: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported managed MySQL action %q", action)
	}
}

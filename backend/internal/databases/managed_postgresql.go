package databases

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

const (
	DefaultManagedPostgreSQLContainer = "devbox-postgresql"
	DefaultManagedPostgreSQLVolume    = "devbox-postgresql-data"
	DefaultManagedPostgreSQLImage     = "postgres:17"

	managedPostgreSQLSecretScope = "managed-postgresql"
	managedPostgreSQLSecretName  = "postgres-password"
)

type ManagedPostgreSQLConfig struct {
	Image                string
	Container            string
	Network              string
	Volume               string
	InitialAdminPassword string
}

type ManagedPostgreSQLManager struct {
	docker  providers.DockerInfrastructureProvider
	secrets secrets.SecretStore
	cfg     ManagedPostgreSQLConfig
}

func NewManagedPostgreSQLManager(dockerProvider providers.DockerInfrastructureProvider, secretStore secrets.SecretStore, cfg ManagedPostgreSQLConfig) *ManagedPostgreSQLManager {
	if cfg.Image == "" {
		cfg.Image = DefaultManagedPostgreSQLImage
	}
	if cfg.Container == "" {
		cfg.Container = DefaultManagedPostgreSQLContainer
	}
	if cfg.Network == "" {
		cfg.Network = DefaultManagedMySQLNetwork
	}
	if cfg.Volume == "" {
		cfg.Volume = DefaultManagedPostgreSQLVolume
	}
	return &ManagedPostgreSQLManager{docker: dockerProvider, secrets: secretStore, cfg: cfg}
}

func (m *ManagedPostgreSQLManager) Ensure(ctx context.Context) error {
	if m == nil || m.docker == nil {
		return errors.New("managed PostgreSQL Docker provider is not configured")
	}
	if m.secrets == nil {
		return ErrSecretsUnavailable
	}
	if err := m.docker.EnsureNetwork(ctx, m.cfg.Network); err != nil {
		return fmt.Errorf("ensure managed PostgreSQL network: %w", err)
	}
	if err := m.docker.EnsureVolume(ctx, m.cfg.Volume); err != nil {
		return fmt.Errorf("ensure managed PostgreSQL volume: %w", err)
	}
	if err := m.docker.EnsureImage(ctx, m.cfg.Image); err != nil {
		return fmt.Errorf("ensure managed PostgreSQL image: %w", err)
	}
	password, err := m.AdminPassword(ctx)
	if err != nil {
		return err
	}
	defer clear(password)

	spec := providers.ContainerSpec{
		Name:          m.cfg.Container,
		Image:         m.cfg.Image,
		RestartPolicy: "unless-stopped",
		Networks:      []string{m.cfg.Network},
		Volumes:       map[string]string{m.cfg.Volume: "/var/lib/postgresql/data"},
		SensitiveEnvironment: map[string]string{
			"POSTGRES_PASSWORD": string(password),
			"POSTGRES_USER":     "postgres",
		},
		Labels: map[string]string{
			"io.devbox.managed-postgresql": "true",
		},
	}
	item, err := m.docker.EnsureContainer(ctx, spec)
	if err != nil {
		return fmt.Errorf("ensure managed PostgreSQL container: %w", err)
	}
	if err := m.docker.ConnectNetwork(ctx, m.cfg.Container, m.cfg.Network); err != nil {
		return fmt.Errorf("reconcile managed PostgreSQL network: %w", err)
	}
	if !strings.EqualFold(item.State, "running") {
		if err := m.docker.Start(ctx, m.cfg.Container); err != nil {
			return fmt.Errorf("start managed PostgreSQL: %w", err)
		}
	}
	return nil
}

func (m *ManagedPostgreSQLManager) AdminPassword(ctx context.Context) ([]byte, error) {
	if m == nil || m.secrets == nil {
		return nil, ErrSecretsUnavailable
	}
	value, err := m.secrets.Get(ctx, managedPostgreSQLSecretScope, managedPostgreSQLSecretName)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, secrets.ErrNotFound) {
		return nil, fmt.Errorf("load managed PostgreSQL admin credential: %w", err)
	}
	password := strings.TrimSpace(m.cfg.InitialAdminPassword)
	if password == "" {
		password, err = GeneratePassword()
		if err != nil {
			return nil, err
		}
	}
	if err := m.secrets.Put(ctx, managedPostgreSQLSecretScope, managedPostgreSQLSecretName, []byte(password)); err != nil {
		return nil, fmt.Errorf("store managed PostgreSQL admin credential: %w", err)
	}
	return []byte(password), nil
}

func (m *ManagedPostgreSQLManager) ApplicationEndpoint() providers.DatabaseEndpoint {
	return providers.DatabaseEndpoint{Host: m.cfg.Container, Port: 5432}
}

func (m *ManagedPostgreSQLManager) Network() string {
	return m.cfg.Network
}

func (m *ManagedPostgreSQLManager) ContainerName() string {
	return m.cfg.Container
}

func (m *ManagedPostgreSQLManager) Image() string {
	return m.cfg.Image
}

func (m *ManagedPostgreSQLManager) Volume() string {
	return m.cfg.Volume
}

func (m *ManagedPostgreSQLManager) ContainerState(ctx context.Context) (installed bool, running bool, err error) {
	if m == nil || m.docker == nil {
		return false, false, errors.New("managed PostgreSQL Docker provider is not configured")
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

func (m *ManagedPostgreSQLManager) Action(ctx context.Context, action string) error {
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
	default:
		return fmt.Errorf("unsupported managed PostgreSQL action %q", action)
	}
}

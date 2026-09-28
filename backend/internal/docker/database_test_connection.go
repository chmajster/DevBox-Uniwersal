package docker

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

const databaseClientImage = "mysql:8.4"

func (p *CLIProvider) TestDatabaseConnection(ctx context.Context, network string, connection providers.DatabaseConnection, password []byte) error {
	if strings.TrimSpace(connection.Host) == "" {
		return fmt.Errorf("%w: database host is required", ErrInvalidInput)
	}
	if err := validateValue(connection.Host, "database host"); err != nil {
		return err
	}
	if connection.Port < 1 || connection.Port > 65535 {
		return fmt.Errorf("%w: database port is out of range", ErrInvalidInput)
	}
	if !safeMySQLIdentifier(connection.Database) || !safeMySQLIdentifier(connection.Username) {
		return fmt.Errorf("%w: invalid database connection identifier", ErrInvalidInput)
	}
	if network != "" {
		if err := validateNetworkRef(network); err != nil {
			return err
		}
		if err := p.EnsureNetwork(ctx, network); err != nil {
			return err
		}
	}
	if err := p.EnsureImage(ctx, databaseClientImage); err != nil {
		return fmt.Errorf("prepare database connectivity client: %w", err)
	}
	envRunner, ok := p.runner.(environmentCommandRunner)
	if !ok {
		return fmt.Errorf("%w: Docker runner does not support secret-safe environment injection", ErrUnavailable)
	}
	args := []string{"run", "--rm"}
	if network != "" {
		args = append(args, "--network", network)
	}
	args = append(args,
		"--env", "MYSQL_PWD",
		databaseClientImage,
		"mysql",
		"--protocol=TCP",
		"--connect-timeout=5",
		"--host", connection.Host,
		"--port", strconv.Itoa(connection.Port),
		"--user", connection.Username,
		"--database", connection.Database,
		"--batch", "--skip-column-names",
		"--execute", "SELECT 1",
	)
	out, _, err := envRunner.RunEnv(ctx, map[string]string{"MYSQL_PWD": string(password)}, args...)
	if err != nil {
		return fmt.Errorf("database connection test failed: %w", err)
	}
	if strings.TrimSpace(string(out)) != "1" {
		return fmt.Errorf("database connection test failed: unexpected SELECT 1 result")
	}
	return nil
}

var _ providers.ContainerDatabaseTester = (*CLIProvider)(nil)

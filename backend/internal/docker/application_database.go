package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func (p *CLIProvider) TestApplicationContainerDatabase(ctx context.Context, container string, connection providers.DatabaseConnection, password []byte) error {
	if err := validateContainerRef(container); err != nil {
		return err
	}
	probe := os.Getenv("DEVBOX_DATABASE_PROBE")
	if probe == "" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		probe = filepath.Join(filepath.Dir(executable), "devbox-dbcheck")
	}
	if _, err := os.Stat(probe); err != nil {
		return fmt.Errorf("SQL probe is unavailable; install devbox-dbcheck or set DEVBOX_DATABASE_PROBE: %w", err)
	}
	info, err := p.Inspect(ctx, container)
	if err != nil {
		return err
	}
	if info.State != "running" {
		return fmt.Errorf("application container is %s; start it before testing SQL", info.State)
	}
	if _, _, err := p.runner.Run(ctx, "cp", probe, container+":/tmp/devbox-dbcheck"); err != nil {
		return fmt.Errorf("copy SQL probe into application: %w", err)
	}
	runner, ok := p.runner.(interface {
		RunInput(context.Context, []byte, ...string) ([]byte, []byte, error)
	})
	if !ok {
		return ErrUnavailable
	}
	data, _ := json.Marshal(map[string]any{"Engine": connection.Engine, "Host": connection.Host, "Port": connection.Port, "Database": connection.Database, "Username": connection.Username, "Password": string(password)})
	defer clear(data)
	out, _, err := runner.RunInput(ctx, data, "exec", "-i", container, "/tmp/devbox-dbcheck")
	if err != nil {
		return fmt.Errorf("application SQL authentication/SELECT 1 failed: %w", err)
	}
	if strings.TrimSpace(string(out)) != "1" {
		return fmt.Errorf("application SQL probe returned an unexpected result")
	}
	return nil
}

func (r execRunner) RunInput(ctx context.Context, input []byte, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), stderr.Bytes(), commandError(r.binary, stderr.String(), err)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

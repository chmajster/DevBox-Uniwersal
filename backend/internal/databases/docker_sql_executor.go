package databases

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

type dockerSQLExecutor struct {
	cfg     MySQLConfig
	secrets secrets.SecretStore
}

func (e *dockerSQLExecutor) run(ctx context.Context, input io.Reader, output io.Writer, client string, args ...string) error {
	password, err := e.secrets.Get(ctx, e.cfg.AdminSecretScope, e.cfg.AdminSecretRef)
	if err != nil {
		return err
	}
	defer clear(password)
	argv := []string{"exec", "-i", "-e", "MYSQL_PWD", e.cfg.DockerContainer, client, "-u", e.cfg.AdminUser}
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, e.cfg.DockerBinary, argv...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+string(password))
	cmd.Stdin = input
	cmd.Stdout = output
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("database Docker command: %s: %w", sanitizeMySQLError(strings.ReplaceAll(stderr.String(), string(password), "***")), err)
	}
	return nil
}
func (e *dockerSQLExecutor) QuerySQL(ctx context.Context, statement string) (string, error) {
	var output bytes.Buffer
	err := e.run(ctx, strings.NewReader(statement), &output, e.cfg.ClientBinary, "--batch", "--skip-column-names", "--raw")
	return output.String(), err
}
func (e *dockerSQLExecutor) ExecSQL(ctx context.Context, statement string) error {
	_, err := e.QuerySQL(ctx, statement)
	return err
}
func (e *dockerSQLExecutor) Dump(ctx context.Context, database string, out io.Writer) error {
	client := "mysqldump"
	if e.cfg.ClientBinary == "mariadb" {
		client = "mariadb-dump"
	}
	return e.run(ctx, nil, out, client, "--single-transaction", "--routines", "--triggers", "--events", "--hex-blob", "--add-drop-table", "--databases", database)
}
func (e *dockerSQLExecutor) Restore(ctx context.Context, in io.Reader) error {
	return e.run(ctx, in, io.Discard, e.cfg.ClientBinary)
}

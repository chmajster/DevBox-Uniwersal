package databases

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

type PostgreSQLConfig struct {
	DockerBinary    string
	Container       string
	ApplicationHost string
	ApplicationPort int
	AdminUser       string
}

type PostgreSQLProvider struct {
	cfg     PostgreSQLConfig
	secrets secrets.SecretStore
}

func NewPostgreSQLProvider(cfg PostgreSQLConfig, secretStore secrets.SecretStore) *PostgreSQLProvider {
	if strings.TrimSpace(cfg.DockerBinary) == "" {
		cfg.DockerBinary = "docker"
	}
	if strings.TrimSpace(cfg.Container) == "" {
		cfg.Container = DefaultManagedPostgreSQLContainer
	}
	if strings.TrimSpace(cfg.ApplicationHost) == "" {
		cfg.ApplicationHost = cfg.Container
	}
	if cfg.ApplicationPort == 0 {
		cfg.ApplicationPort = 5432
	}
	if strings.TrimSpace(cfg.AdminUser) == "" {
		cfg.AdminUser = "postgres"
	}
	return &PostgreSQLProvider{cfg: cfg, secrets: secretStore}
}

// Endpoint is the address application containers use on the shared Docker network.
func (p *PostgreSQLProvider) Endpoint() (string, int) {
	return p.cfg.ApplicationHost, p.cfg.ApplicationPort
}

func (p *PostgreSQLProvider) ApplicationEndpoint() providers.DatabaseEndpoint {
	return providers.DatabaseEndpoint{Host: p.cfg.ApplicationHost, Port: p.cfg.ApplicationPort}
}

func (p *PostgreSQLProvider) Validate(ctx context.Context) error {
	return p.Health(ctx)
}

func (p *PostgreSQLProvider) Health(ctx context.Context) error {
	// The entrypoint's temporary initialization server accepts Unix sockets,
	// but has no TCP listener. Wait for the final server used by applications.
	ready := exec.CommandContext(ctx, p.cfg.DockerBinary, "exec", p.cfg.Container, "pg_isready", "-h", "127.0.0.1", "-U", p.cfg.AdminUser, "-d", "postgres")
	if err := ready.Run(); err != nil {
		return fmt.Errorf("PostgreSQL TCP server is not ready: %w", err)
	}
	out, err := p.query(ctx, "postgres", "SELECT 1;")
	if err != nil {
		return fmt.Errorf("postgresql health check failed: %w", err)
	}
	if strings.TrimSpace(out) != "1" {
		return errors.New("postgresql health check returned unexpected result")
	}
	return nil
}

func (p *PostgreSQLProvider) Status(ctx context.Context) MySQLStatus {
	version, err := p.Version(ctx)
	if err != nil {
		return MySQLStatus{Running: false, ConnectionState: "unavailable"}
	}
	return MySQLStatus{
		Version:         version,
		Running:         true,
		ConnectionState: "connected",
		ApplicationHost: p.cfg.ApplicationHost,
		ApplicationPort: p.cfg.ApplicationPort,
	}
}

func (p *PostgreSQLProvider) Version(ctx context.Context) (string, error) {
	out, err := p.query(ctx, "postgres", "SHOW server_version;")
	if err != nil {
		return "", fmt.Errorf("query PostgreSQL version: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (p *PostgreSQLProvider) CreateDatabase(ctx context.Context, spec providers.DatabaseSpec) error {
	if spec.Engine != "" && spec.Engine != "postgresql" && spec.Engine != "postgres" {
		return fmt.Errorf("unsupported database engine %q", spec.Engine)
	}
	name, err := pgQuoteIdentifier(spec.Name)
	if err != nil {
		return err
	}
	encoding := strings.ToUpper(strings.TrimSpace(spec.Charset))
	if encoding == "" {
		encoding = "UTF8"
	}
	switch encoding {
	case "UTF8", "LATIN1":
	default:
		return fmt.Errorf("unsupported PostgreSQL encoding %q", spec.Charset)
	}
	if err := p.execSQL(ctx, "postgres", "CREATE DATABASE "+name+" ENCODING "+quoteLiteral(encoding)+";"); err != nil {
		return fmt.Errorf("create PostgreSQL database: %w", err)
	}
	if err := p.execSQL(ctx, "postgres", "REVOKE ALL ON DATABASE "+name+" FROM PUBLIC;"); err != nil {
		return err
	}
	if err := p.execSQL(ctx, spec.Name, "REVOKE CREATE ON SCHEMA public FROM PUBLIC;"); err != nil {
		return err
	}
	return nil
}

func (p *PostgreSQLProvider) DeleteDatabase(ctx context.Context, name string) error {
	quoted, err := pgQuoteIdentifier(name)
	if err != nil {
		return err
	}
	terminate := "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=" + quoteLiteral(name) + " AND pid <> pg_backend_pid();"
	if err := p.execSQL(ctx, "postgres", terminate); err != nil {
		return fmt.Errorf("terminate PostgreSQL database sessions: %w", err)
	}
	if err := p.execSQL(ctx, "postgres", "DROP DATABASE IF EXISTS "+quoted+";"); err != nil {
		return fmt.Errorf("delete PostgreSQL database: %w", err)
	}
	return nil
}

func (p *PostgreSQLProvider) CreateUser(ctx context.Context, username, secretRef string) error {
	if p.secrets == nil {
		return errors.New("secret store is not configured")
	}
	role, err := pgQuoteRole(username)
	if err != nil {
		return err
	}
	password, err := p.secrets.Get(ctx, "database-user", secretRef)
	if err != nil {
		return fmt.Errorf("load PostgreSQL user secret: %w", err)
	}
	defer clear(password)
	if err := p.execSQL(ctx, "postgres", "CREATE ROLE "+role+" LOGIN PASSWORD "+quoteLiteral(string(password))+";"); err != nil {
		return fmt.Errorf("create PostgreSQL user: %w", err)
	}
	return nil
}

func (p *PostgreSQLProvider) DeleteUser(ctx context.Context, username string) error {
	role, err := pgQuoteRole(username)
	if err != nil {
		return err
	}
	if err := p.execSQL(ctx, "postgres", "DROP ROLE IF EXISTS "+role+";"); err != nil {
		return fmt.Errorf("delete PostgreSQL user: %w", err)
	}
	return nil
}

func (p *PostgreSQLProvider) ChangePassword(ctx context.Context, username, password string) error {
	role, err := pgQuoteRole(username)
	if err != nil {
		return err
	}
	if err := p.execSQL(ctx, "postgres", "ALTER ROLE "+role+" PASSWORD "+quoteLiteral(password)+";"); err != nil {
		return fmt.Errorf("change PostgreSQL user password: %w", err)
	}
	return nil
}

func (p *PostgreSQLProvider) Grant(ctx context.Context, database, username string, privileges []string) error {
	db, err := pgQuoteIdentifier(database)
	if err != nil {
		return err
	}
	role, err := pgQuoteRole(username)
	if err != nil {
		return err
	}
	normalized, err := normalizePostgreSQLPrivileges(privileges)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return errors.New("at least one PostgreSQL privilege is required")
	}
	if err := p.execSQL(ctx, "postgres", "GRANT CONNECT ON DATABASE "+db+" TO "+role+";"); err != nil {
		return fmt.Errorf("grant PostgreSQL database connect: %w", err)
	}
	statements := []string{"GRANT USAGE ON SCHEMA public TO " + role + ";"}
	tablePrivileges := pgTablePrivileges(normalized)
	if len(tablePrivileges) > 0 {
		list := strings.Join(tablePrivileges, ", ")
		statements = append(statements,
			"GRANT "+list+" ON ALL TABLES IN SCHEMA public TO "+role+";",
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT "+list+" ON TABLES TO "+role+";",
		)
	}
	if containsString(normalized, "INSERT") {
		statements = append(statements,
			"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO "+role+";",
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO "+role+";",
		)
	}
	if containsString(normalized, "CREATE") {
		statements = append(statements, "GRANT CREATE ON SCHEMA public TO "+role+";")
	}
	if containsString(normalized, "EXECUTE") {
		statements = append(statements,
			"GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO "+role+";",
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO "+role+";",
		)
	}
	if err := p.execSQL(ctx, database, strings.Join(statements, "\n")); err != nil {
		return fmt.Errorf("grant PostgreSQL privileges: %w", err)
	}
	return nil
}

func (p *PostgreSQLProvider) Revoke(ctx context.Context, database, username string) error {
	db, err := pgQuoteIdentifier(database)
	if err != nil {
		return err
	}
	role, err := pgQuoteRole(username)
	if err != nil {
		return err
	}
	statements := []string{
		"REASSIGN OWNED BY " + role + " TO " + pgQuoteIdentifierMust(p.cfg.AdminUser) + ";",
		"DROP OWNED BY " + role + ";",
		"REVOKE ALL PRIVILEGES ON SCHEMA public FROM " + role + ";",
	}
	if err := p.execSQL(ctx, database, strings.Join(statements, "\n")); err != nil {
		return fmt.Errorf("revoke PostgreSQL owned privileges: %w", err)
	}
	if err := p.execSQL(ctx, "postgres", "REVOKE ALL PRIVILEGES ON DATABASE "+db+" FROM "+role+";"); err != nil {
		return fmt.Errorf("revoke PostgreSQL database privileges: %w", err)
	}
	return nil
}

func (p *PostgreSQLProvider) RevokePrivileges(ctx context.Context, database, username string, privileges []string) error {
	role, err := pgQuoteRole(username)
	if err != nil {
		return err
	}
	normalized, err := normalizePostgreSQLPrivileges(privileges)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return errors.New("at least one PostgreSQL privilege is required")
	}
	var statements []string
	tablePrivileges := pgTablePrivileges(normalized)
	if len(tablePrivileges) > 0 {
		list := strings.Join(tablePrivileges, ", ")
		statements = append(statements,
			"REVOKE "+list+" ON ALL TABLES IN SCHEMA public FROM "+role+";",
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE "+list+" ON TABLES FROM "+role+";",
		)
	}
	if containsString(normalized, "INSERT") {
		statements = append(statements,
			"REVOKE USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public FROM "+role+";",
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE USAGE, SELECT ON SEQUENCES FROM "+role+";",
		)
	}
	if containsString(normalized, "CREATE") {
		statements = append(statements, "REVOKE CREATE ON SCHEMA public FROM "+role+";")
	}
	if containsString(normalized, "EXECUTE") {
		statements = append(statements,
			"REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA public FROM "+role+";",
			"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE EXECUTE ON FUNCTIONS FROM "+role+";",
		)
	}
	if len(statements) == 0 {
		return nil
	}
	if err := p.execSQL(ctx, database, strings.Join(statements, "\n")); err != nil {
		return fmt.Errorf("revoke PostgreSQL privileges: %w", err)
	}
	return nil
}

func (p *PostgreSQLProvider) Size(ctx context.Context, database string) (int64, error) {
	if err := ValidateIdentifier(database); err != nil {
		return 0, err
	}
	out, err := p.query(ctx, "postgres", "SELECT pg_database_size("+quoteLiteral(database)+");")
	if err != nil {
		return 0, fmt.Errorf("query PostgreSQL database size: %w", err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse PostgreSQL database size: %w", err)
	}
	return size, nil
}

func (p *PostgreSQLProvider) DumpDatabase(ctx context.Context, database string, out io.Writer) error {
	if err := ValidateIdentifier(database); err != nil {
		return err
	}
	args := []string{"exec", p.cfg.Container, "pg_dump", "-U", p.cfg.AdminUser, "--clean", "--if-exists", "--no-owner", "--no-privileges", "-d", database}
	cmd := exec.CommandContext(ctx, p.cfg.DockerBinary, args...)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return postgreSQLCommandError("PostgreSQL backup", stderr.String(), err)
	}
	return nil
}

func (p *PostgreSQLProvider) RestoreDatabase(ctx context.Context, database string, in io.Reader) error {
	if err := ValidateIdentifier(database); err != nil {
		return err
	}
	args := []string{"exec", "-i", p.cfg.Container, "psql", "-X", "-U", p.cfg.AdminUser, "-d", database, "-v", "ON_ERROR_STOP=1"}
	cmd := exec.CommandContext(ctx, p.cfg.DockerBinary, args...)
	cmd.Stdin = in
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return postgreSQLCommandError("PostgreSQL restore", stderr.String(), err)
	}
	return nil
}

func (p *PostgreSQLProvider) execSQL(ctx context.Context, database, statement string) error {
	_, err := p.runPSQL(ctx, database, statement)
	return err
}

func (p *PostgreSQLProvider) query(ctx context.Context, database, statement string) (string, error) {
	return p.runPSQL(ctx, database, statement)
}

func (p *PostgreSQLProvider) runPSQL(ctx context.Context, database, statement string) (string, error) {
	if err := ValidateIdentifier(database); err != nil {
		return "", err
	}
	args := []string{
		"exec", p.cfg.Container,
		"psql", "-X", "-U", p.cfg.AdminUser, "-d", database,
		"-v", "ON_ERROR_STOP=1", "-A", "-t", "-q",
	}
	args = append([]string{"exec", "-i"}, args[1:]...)
	cmd := exec.CommandContext(ctx, p.cfg.DockerBinary, args...)
	cmd.Stdin = strings.NewReader(statement)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		masked := regexp.MustCompile(`(?i)PASSWORD\s+'(?:''|[^'])*'`).ReplaceAllString(stderr.String(), "PASSWORD '[REDACTED]'")
		return "", postgreSQLCommandError("psql", masked, err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func normalizePostgreSQLPrivileges(privileges []string) ([]string, error) {
	allowed := map[string]bool{
		"SELECT":     true,
		"INSERT":     true,
		"UPDATE":     true,
		"DELETE":     true,
		"CREATE":     true,
		"REFERENCES": true,
		"TRIGGER":    true,
		"EXECUTE":    true,
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(privileges))
	for _, privilege := range privileges {
		value := strings.ToUpper(strings.TrimSpace(privilege))
		if !allowed[value] {
			return nil, fmt.Errorf("%w for PostgreSQL: %s", ErrInvalidPrivilege, privilege)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out, nil
}

func pgTablePrivileges(privileges []string) []string {
	tableAllowed := map[string]bool{
		"SELECT":     true,
		"INSERT":     true,
		"UPDATE":     true,
		"DELETE":     true,
		"REFERENCES": true,
		"TRIGGER":    true,
	}
	out := make([]string, 0, len(privileges))
	for _, privilege := range privileges {
		if tableAllowed[privilege] {
			out = append(out, privilege)
		}
	}
	return out
}

func pgQuoteIdentifier(value string) (string, error) {
	if err := ValidateIdentifier(value); err != nil {
		return "", err
	}
	return "\"" + strings.ReplaceAll(value, "\"", "\"\"") + "\"", nil
}

func pgQuoteIdentifierMust(value string) string {
	quoted, err := pgQuoteIdentifier(value)
	if err != nil {
		return "\"" + strings.ReplaceAll(value, "\"", "\"\"") + "\""
	}
	return quoted
}

func pgQuoteRole(value string) (string, error) {
	if err := validateUsername(value); err != nil {
		return "", err
	}
	return pgQuoteIdentifier(value)
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func postgreSQLCommandError(operation, raw string, runErr error) error {
	message := strings.TrimSpace(raw)
	if len(message) > 800 {
		message = message[:800] + "…"
	}
	if message == "" {
		return fmt.Errorf("%s command failed: %w", operation, runErr)
	}
	return fmt.Errorf("%s command failed: %s: %w", operation, message, runErr)
}

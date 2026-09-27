package databases

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

var _ providers.DatabaseProvider = (*MySQLProvider)(nil)

var (
	ErrInvalidIdentifier = errors.New("invalid SQL identifier")
	ErrInvalidPrivilege  = errors.New("invalid database privilege")
)

type MySQLConfig struct {
	Host          string
	Port          int
	AdminUser     string
	AdminPassword string
	ApplicationHost string
	MySQLBinary   string
	DumpBinary    string
}

type mysqlExecutor interface {
	ExecSQL(context.Context, string) error
	QuerySQL(context.Context, string) (string, error)
	Dump(context.Context, string, io.Writer) error
	Restore(context.Context, io.Reader) error
}

type cliMySQLExecutor struct {
	cfg MySQLConfig
}

type MySQLProvider struct {
	cfg     MySQLConfig
	secrets secrets.SecretStore
	exec    mysqlExecutor
}

func NewMySQLProvider(cfg MySQLConfig, secretStore secrets.SecretStore) *MySQLProvider {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 3306
	}
	if cfg.AdminUser == "" {
		cfg.AdminUser = "devbox_admin"
	}
	if cfg.ApplicationHost == "" {
		cfg.ApplicationHost = "%"
	}
	if cfg.MySQLBinary == "" {
		cfg.MySQLBinary = "mysql"
	}
	if cfg.DumpBinary == "" {
		cfg.DumpBinary = "mysqldump"
	}
	return &MySQLProvider{cfg: cfg, secrets: secretStore, exec: &cliMySQLExecutor{cfg: cfg}}
}

func (p *MySQLProvider) Endpoint() (string, int) {
	return p.cfg.Host, p.cfg.Port
}

func (p *MySQLProvider) Validate(ctx context.Context) error {
	_, err := p.Version(ctx)
	return err
}

func (p *MySQLProvider) Health(ctx context.Context) error {
	_, err := p.exec.QuerySQL(ctx, "SELECT 1;")
	if err != nil {
		return fmt.Errorf("mysql health check failed: %w", err)
	}
	return nil
}

func (p *MySQLProvider) Status(ctx context.Context) MySQLStatus {
	version, err := p.Version(ctx)
	if err != nil {
		return MySQLStatus{Running: false, ConnectionState: "unavailable"}
	}
	return MySQLStatus{Version: version, Running: true, ConnectionState: "connected"}
}

func (p *MySQLProvider) Version(ctx context.Context) (string, error) {
	out, err := p.exec.QuerySQL(ctx, "SELECT VERSION();")
	if err != nil {
		return "", fmt.Errorf("query mysql version: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (p *MySQLProvider) CreateDatabase(ctx context.Context, spec providers.DatabaseSpec) error {
	if spec.Engine != "" && spec.Engine != "mysql" && spec.Engine != "mariadb" {
		return fmt.Errorf("unsupported database engine %q", spec.Engine)
	}
	name, err := quoteIdentifier(spec.Name)
	if err != nil {
		return err
	}
	charset := spec.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	switch charset {
	case "utf8mb4", "utf8", "latin1":
	default:
		return fmt.Errorf("unsupported database charset %q", charset)
	}
	if err := p.exec.ExecSQL(ctx, "CREATE DATABASE "+name+" CHARACTER SET "+charset+";"); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	return nil
}

func (p *MySQLProvider) DeleteDatabase(ctx context.Context, name string) error {
	quoted, err := quoteIdentifier(name)
	if err != nil {
		return err
	}
	if err := p.exec.ExecSQL(ctx, "DROP DATABASE IF EXISTS "+quoted+";"); err != nil {
		return fmt.Errorf("delete database: %w", err)
	}
	return nil
}

func (p *MySQLProvider) CreateUser(ctx context.Context, username, secretRef string) error {
	if p.secrets == nil {
		return errors.New("secret store is not configured")
	}
	if err := validateUsername(username); err != nil {
		return err
	}
	password, err := p.secrets.Get(ctx, "database-user", secretRef)
	if err != nil {
		return fmt.Errorf("load database user secret: %w", err)
	}
	defer clear(password)
	account, err := p.account(username)
	if err != nil {
		return err
	}
	sql := "CREATE USER " + account + " IDENTIFIED BY " + quoteLiteral(string(password)) + ";"
	if err := p.exec.ExecSQL(ctx, sql); err != nil {
		return fmt.Errorf("create database user: %w", err)
	}
	return nil
}

func (p *MySQLProvider) DeleteUser(ctx context.Context, username string) error {
	account, err := p.account(username)
	if err != nil {
		return err
	}
	if err := p.exec.ExecSQL(ctx, "DROP USER IF EXISTS "+account+";"); err != nil {
		return fmt.Errorf("delete database user: %w", err)
	}
	return nil
}

func (p *MySQLProvider) ChangePassword(ctx context.Context, username, password string) error {
	account, err := p.account(username)
	if err != nil {
		return err
	}
	if err := p.exec.ExecSQL(ctx, "ALTER USER "+account+" IDENTIFIED BY "+quoteLiteral(password)+";"); err != nil {
		return fmt.Errorf("change database user password: %w", err)
	}
	return nil
}

func (p *MySQLProvider) Grant(ctx context.Context, database, username string, privileges []string) error {
	return p.changePrivileges(ctx, "GRANT", database, username, privileges)
}

func (p *MySQLProvider) Revoke(ctx context.Context, database, username string) error {
	return p.RevokePrivileges(ctx, database, username, defaultPrivileges())
}

func (p *MySQLProvider) RevokePrivileges(ctx context.Context, database, username string, privileges []string) error {
	return p.changePrivileges(ctx, "REVOKE", database, username, privileges)
}

func (p *MySQLProvider) changePrivileges(ctx context.Context, action, database, username string, privileges []string) error {
	db, err := quoteIdentifier(database)
	if err != nil {
		return err
	}
	account, err := p.account(username)
	if err != nil {
		return err
	}
	normalized, err := normalizePrivileges(privileges)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return errors.New("at least one privilege is required")
	}
	var sql string
	if action == "GRANT" {
		sql = "GRANT " + strings.Join(normalized, ", ") + " ON " + db + ".* TO " + account + ";"
	} else {
		sql = "REVOKE " + strings.Join(normalized, ", ") + " ON " + db + ".* FROM " + account + ";"
	}
	if err := p.exec.ExecSQL(ctx, sql); err != nil {
		return fmt.Errorf("%s database privileges: %w", strings.ToLower(action), err)
	}
	return nil
}

func (p *MySQLProvider) Size(ctx context.Context, database string) (int64, error) {
	if err := ValidateIdentifier(database); err != nil {
		return 0, err
	}
	query := "SELECT COALESCE(SUM(data_length + index_length),0) FROM information_schema.tables WHERE table_schema=" + quoteLiteral(database) + ";"
	out, err := p.exec.QuerySQL(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("query database size: %w", err)
	}
	value := strings.TrimSpace(out)
	if value == "" || strings.EqualFold(value, "NULL") {
		return 0, nil
	}
	size, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse database size: %w", err)
	}
	return size, nil
}

func (p *MySQLProvider) DumpDatabase(ctx context.Context, database string, out io.Writer) error {
	if err := ValidateIdentifier(database); err != nil {
		return err
	}
	if err := p.exec.Dump(ctx, database, out); err != nil {
		return fmt.Errorf("database backup failed: %w", err)
	}
	return nil
}

func (p *MySQLProvider) RestoreDatabase(ctx context.Context, database string, in io.Reader) error {
	if err := ValidateIdentifier(database); err != nil {
		return err
	}
	if err := p.exec.Restore(ctx, in); err != nil {
		return fmt.Errorf("database restore failed: %w", err)
	}
	return nil
}

func (p *MySQLProvider) account(username string) (string, error) {
	if err := validateUsername(username); err != nil {
		return "", err
	}
	if strings.ContainsAny(p.cfg.ApplicationHost, "'\r\n\x00") {
		return "", errors.New("invalid application database host")
	}
	return quoteLiteral(username) + "@" + quoteLiteral(p.cfg.ApplicationHost), nil
}

func ValidateIdentifier(value string) error {
	if value == "" || len(value) > 64 {
		return ErrInvalidIdentifier
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return ErrInvalidIdentifier
		}
	}
	return nil
}

func quoteIdentifier(value string) (string, error) {
	if err := ValidateIdentifier(value); err != nil {
		return "", err
	}
	return "`" + value + "`", nil
}

func validateUsername(value string) error {
	if value == "" || len(value) > 32 {
		return ErrInvalidIdentifier
	}
	return ValidateIdentifier(value)
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func normalizePrivileges(privileges []string) ([]string, error) {
	allowed := map[string]bool{
		"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
		"CREATE": true, "DROP": true, "INDEX": true, "ALTER": true,
		"REFERENCES": true, "CREATE TEMPORARY TABLES": true, "LOCK TABLES": true,
		"EXECUTE": true, "CREATE VIEW": true, "SHOW VIEW": true, "TRIGGER": true,
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(privileges))
	for _, privilege := range privileges {
		value := strings.ToUpper(strings.TrimSpace(privilege))
		if !allowed[value] {
			return nil, fmt.Errorf("%w: %s", ErrInvalidPrivilege, privilege)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out, nil
}

func defaultPrivileges() []string {
	return []string{
		"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "INDEX", "ALTER",
		"REFERENCES", "CREATE TEMPORARY TABLES", "LOCK TABLES", "CREATE VIEW", "SHOW VIEW", "TRIGGER",
	}
}

func (e *cliMySQLExecutor) ExecSQL(ctx context.Context, statement string) error {
	_, err := e.runMySQL(ctx, statement)
	return err
}

func (e *cliMySQLExecutor) QuerySQL(ctx context.Context, statement string) (string, error) {
	return e.runMySQL(ctx, statement)
}

func (e *cliMySQLExecutor) runMySQL(ctx context.Context, statement string) (string, error) {
	defaults, cleanup, err := e.defaultsFile()
	if err != nil {
		return "", err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, e.cfg.MySQLBinary, "--defaults-extra-file="+defaults, "--batch", "--skip-column-names", "--raw")
	cmd.Stdin = strings.NewReader(statement)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("mysql command failed: %w", err)
	}
	return stdout.String(), nil
}

func (e *cliMySQLExecutor) Dump(ctx context.Context, database string, out io.Writer) error {
	defaults, cleanup, err := e.defaultsFile()
	if err != nil {
		return err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, e.cfg.DumpBinary,
		"--defaults-extra-file="+defaults,
		"--single-transaction",
		"--routines",
		"--triggers",
		"--events",
		"--hex-blob",
		"--add-drop-table",
		"--databases",
		database,
	)
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysqldump command failed: %w", err)
	}
	return nil
}

func (e *cliMySQLExecutor) Restore(ctx context.Context, in io.Reader) error {
	defaults, cleanup, err := e.defaultsFile()
	if err != nil {
		return err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, e.cfg.MySQLBinary, "--defaults-extra-file="+defaults)
	cmd.Stdin = in
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysql restore command failed: %w", err)
	}
	return nil
}

func (e *cliMySQLExecutor) defaultsFile() (string, func(), error) {
	if strings.ContainsAny(e.cfg.Host, "\r\n") || strings.ContainsAny(e.cfg.AdminUser, "\r\n") || strings.ContainsAny(e.cfg.AdminPassword, "\r\n") {
		return "", func() {}, errors.New("mysql connection settings contain invalid newline")
	}
	file, err := os.CreateTemp("", "devbox-mysql-client-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create mysql credentials file: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("secure mysql credentials file: %w", err)
	}
	_, err = fmt.Fprintf(file, "[client]\nhost=%s\nport=%d\nuser=%s\npassword=%s\nprotocol=tcp\n", e.cfg.Host, e.cfg.Port, e.cfg.AdminUser, e.cfg.AdminPassword)
	closeErr := file.Close()
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write mysql credentials file: %w", err)
	}
	if closeErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close mysql credentials file: %w", closeErr)
	}
	return path, cleanup, nil
}

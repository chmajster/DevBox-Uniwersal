package databases

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

var databaseServiceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type endpointDatabaseEngine interface {
	AdminEndpoint() providers.DatabaseEndpoint
	ApplicationEndpoint() providers.DatabaseEndpoint
	TestConnection(context.Context, providers.DatabaseConnection, []byte) error
}

func bindingSecretScope(projectID string) string {
	return "project-database/" + projectID
}

const dockerHostInternal = "host.docker.internal"

func applicationDatabaseHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return dockerHostInternal
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && ip.IsLoopback() {
		return dockerHostInternal
	}
	return host
}

func requiresDockerHostGateway(connection providers.DatabaseConnection) bool {
	return connection.Mode == DatabaseModeExternal &&
		strings.EqualFold(strings.TrimSpace(connection.Host), dockerHostInternal)
}

func (s *Service) GetDatabaseBinding(ctx context.Context, projectID string) (DatabaseBinding, error) {
	if _, err := s.repo.ProjectByID(ctx, projectID); err != nil {
		return DatabaseBinding{}, err
	}
	item, err := s.repo.DatabaseBindingByProject(ctx, projectID)
	if errors.Is(err, ErrBindingNotFound) {
		return DatabaseBinding{ProjectID: projectID, Mode: DatabaseModeNone}, nil
	}
	if err != nil {
		return DatabaseBinding{}, err
	}
	return s.decorateBinding(ctx, item)
}

func (s *Service) UpdateDatabaseBinding(ctx context.Context, projectID string, input DatabaseBindingInput, actor, remote *string) (DatabaseBinding, error) {
	if s.secrets == nil {
		return DatabaseBinding{}, ErrSecretsUnavailable
	}
	if _, err := s.repo.ProjectByID(ctx, projectID); err != nil {
		return DatabaseBinding{}, err
	}
	input.ApplicationService = strings.TrimSpace(input.ApplicationService)
	input.ComposeService = strings.TrimSpace(input.ComposeService)
	input.Engine = strings.ToLower(strings.TrimSpace(input.Engine))
	input.Host = strings.TrimSpace(input.Host)
	input.Database = strings.TrimSpace(input.Database)
	input.Username = strings.TrimSpace(input.Username)
	if input.Engine == "" {
		input.Engine = "mysql"
	}
	if input.Port == 0 {
		input.Port = 3306
	}
	if err := validateBindingInput(input); err != nil {
		return DatabaseBinding{}, err
	}

	existing, err := s.repo.DatabaseBindingByProject(ctx, projectID)
	if err != nil && !errors.Is(err, ErrBindingNotFound) {
		return DatabaseBinding{}, err
	}
	if input.Mode == DatabaseModeNone {
		if err == nil {
			if existing.SecretRef != "" {
				_ = s.secrets.Delete(ctx, bindingSecretScope(projectID), existing.SecretRef)
			}
			if err := s.repo.DeleteDatabaseBinding(ctx, projectID); err != nil {
				return DatabaseBinding{}, err
			}
		}
		s.recordAudit(ctx, actor, "database_binding.delete", "project", &projectID, map[string]any{"mode": "none"}, remote)
		return DatabaseBinding{ProjectID: projectID, Mode: DatabaseModeNone}, nil
	}

	now := time.Now().UTC()
	item := DatabaseBinding{
		ID:                 newID(),
		ProjectID:          projectID,
		Mode:               input.Mode,
		ApplicationService: input.ApplicationService,
		ComposeService:     input.ComposeService,
		Engine:             input.Engine,
		Host:               input.Host,
		Port:               input.Port,
		Database:           input.Database,
		Username:           input.Username,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err == nil {
		item.ID = existing.ID
		item.CreatedAt = existing.CreatedAt
	}

	switch input.Mode {
	case DatabaseModeManaged:
		database, err := s.repo.DatabaseByProject(ctx, projectID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return DatabaseBinding{}, errors.New("database binding is configured but no database exists")
			}
			return DatabaseBinding{}, err
		}
		users, err := s.repo.UsersByDatabase(ctx, database.ID)
		if err != nil {
			return DatabaseBinding{}, err
		}
		if len(users) == 0 || users[0].SecretRef == "" {
			return DatabaseBinding{}, errors.New("database credentials are unavailable")
		}
		item.DatabaseID = &database.ID
		item.Engine = database.Engine
		item.Port = 3306
		item.Host = ""
		item.Database = ""
		item.Username = ""
		item.SecretRef = ""
	case DatabaseModeCompose, DatabaseModeExternal:
		if input.Password != "" || input.PasswordProvided {
			item.SecretRef = newID()
			if err := s.secrets.Put(ctx, bindingSecretScope(projectID), item.SecretRef, []byte(input.Password)); err != nil {
				return DatabaseBinding{}, fmt.Errorf("store database binding credential: %w", err)
			}
		} else if err == nil && existing.SecretRef != "" && (existing.Mode == DatabaseModeCompose || existing.Mode == DatabaseModeExternal) {
			item.SecretRef = existing.SecretRef
		} else {
			return DatabaseBinding{}, errors.New("database credentials are unavailable")
		}
	}

	if err := s.repo.UpsertDatabaseBinding(ctx, item); err != nil {
		if (input.Password != "" || input.PasswordProvided) && item.SecretRef != "" && (err != nil || existing.SecretRef != item.SecretRef) {
			_ = s.secrets.Delete(ctx, bindingSecretScope(projectID), item.SecretRef)
		}
		return DatabaseBinding{}, err
	}
	if err == nil && existing.SecretRef != "" && existing.SecretRef != item.SecretRef {
		_ = s.secrets.Delete(ctx, bindingSecretScope(projectID), existing.SecretRef)
	}
	s.recordAudit(ctx, actor, "database_binding.update", "project", &projectID, map[string]any{"mode": item.Mode}, remote)
	return s.decorateBinding(ctx, item)
}

func (s *Service) RotateProjectDatabasePassword(ctx context.Context, projectID string, actor, remote *string) error {
	binding, err := s.repo.DatabaseBindingByProject(ctx, projectID)
	if err != nil {
		return err
	}
	if binding.Mode != DatabaseModeManaged || binding.DatabaseID == nil {
		return errors.New("password rotation is only supported for managed project databases")
	}
	users, err := s.repo.UsersByDatabase(ctx, *binding.DatabaseID)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return errors.New("database credentials are unavailable")
	}
	if _, err := s.ChangeUserPassword(ctx, users[0].ID, nil, actor, remote); err != nil {
		return err
	}
	return nil
}

func (s *Service) DeleteDatabaseBinding(ctx context.Context, projectID string, actor, remote *string) error {
	item, err := s.repo.DatabaseBindingByProject(ctx, projectID)
	if errors.Is(err, ErrBindingNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.repo.DeleteDatabaseBinding(ctx, projectID); err != nil {
		return err
	}
	if item.SecretRef != "" && s.secrets != nil {
		_ = s.secrets.Delete(ctx, bindingSecretScope(projectID), item.SecretRef)
	}
	s.recordAudit(ctx, actor, "database_binding.delete", "project", &projectID, map[string]any{"mode": item.Mode}, remote)
	return nil
}

func (s *Service) ResolveApplicationConnection(ctx context.Context, projectID string) (providers.DatabaseConnection, error) {
	binding, err := s.repo.DatabaseBindingByProject(ctx, projectID)
	if errors.Is(err, ErrBindingNotFound) {
		return providers.DatabaseConnection{Mode: DatabaseModeNone}, nil
	}
	if err != nil {
		return providers.DatabaseConnection{}, err
	}
	switch binding.Mode {
	case DatabaseModeManaged:
		if binding.DatabaseID == nil {
			return providers.DatabaseConnection{}, errors.New("database binding is configured but no database exists")
		}
		database, err := s.repo.DatabaseByID(ctx, *binding.DatabaseID)
		if err != nil {
			return providers.DatabaseConnection{}, errors.New("database binding is configured but no database exists")
		}
		users, err := s.repo.UsersByDatabase(ctx, database.ID)
		if err != nil {
			return providers.DatabaseConnection{}, err
		}
		if len(users) == 0 || users[0].SecretRef == "" {
			return providers.DatabaseConnection{}, errors.New("database credentials are unavailable")
		}
		engine, ok := s.engine.(endpointDatabaseEngine)
		if !ok {
			return providers.DatabaseConnection{}, errors.New("cannot resolve application database endpoint")
		}
		endpoint := engine.ApplicationEndpoint()
		if endpoint.Host == "" || endpoint.Port == 0 {
			return providers.DatabaseConnection{}, errors.New("cannot resolve application database endpoint")
		}
		return providers.DatabaseConnection{
			Engine: database.Engine, Host: endpoint.Host, Port: endpoint.Port, Database: database.Name,
			Username: users[0].Username, SecretRef: users[0].SecretRef, Mode: DatabaseModeManaged,
		}, nil
	case DatabaseModeCompose:
		if binding.ComposeService == "" {
			return providers.DatabaseConnection{}, errors.New("Compose database service is not selected")
		}
		return providers.DatabaseConnection{
			Engine: binding.Engine, Host: binding.ComposeService, Port: binding.Port, Database: binding.Database,
			Username: binding.Username, SecretRef: binding.SecretRef, Mode: DatabaseModeCompose,
		}, nil
	case DatabaseModeExternal:
		return providers.DatabaseConnection{
			Engine: binding.Engine, Host: applicationDatabaseHost(binding.Host), Port: binding.Port, Database: binding.Database,
			Username: binding.Username, SecretRef: binding.SecretRef, Mode: DatabaseModeExternal,
		}, nil
	case DatabaseModeNone:
		return providers.DatabaseConnection{Mode: DatabaseModeNone}, nil
	default:
		return providers.DatabaseConnection{}, fmt.Errorf("unsupported database binding mode %q", binding.Mode)
	}
}

func (s *Service) ResolveRuntimeDatabase(ctx context.Context, projectID string) (providers.ProjectDatabaseRuntime, error) {
	connection, err := s.ResolveApplicationConnection(ctx, projectID)
	if err != nil {
		return providers.ProjectDatabaseRuntime{}, err
	}
	result := providers.ProjectDatabaseRuntime{
		Connection:  connection,
		HostGateway: requiresDockerHostGateway(connection),
	}
	if connection.Mode == DatabaseModeNone {
		return result, nil
	}
	binding, err := s.repo.DatabaseBindingByProject(ctx, projectID)
	if err != nil {
		return providers.ProjectDatabaseRuntime{}, err
	}
	result.ApplicationService = binding.ApplicationService
	result.DatabaseService = binding.ComposeService
	switch connection.Mode {
	case DatabaseModeManaged:
		if err := s.ensureManagedReady(ctx); err != nil {
			return providers.ProjectDatabaseRuntime{}, err
		}
		result.Network = DefaultManagedMySQLNetwork
		if s.managed != nil {
			result.Network = s.managed.Network()
		}
		result.Secret, err = s.secrets.Get(ctx, "database-user", connection.SecretRef)
	case DatabaseModeCompose, DatabaseModeExternal:
		result.Secret, err = s.secrets.Get(ctx, bindingSecretScope(projectID), connection.SecretRef)
	}
	if err != nil {
		return providers.ProjectDatabaseRuntime{}, errors.New("database credentials are unavailable")
	}
	return result, nil
}

func (s *Service) TestApplicationConnection(ctx context.Context, projectID string) error {
	runtime, err := s.ResolveRuntimeDatabase(ctx, projectID)
	if err != nil {
		return err
	}
	if runtime.Connection.Mode == DatabaseModeNone {
		return errors.New("project does not use a database")
	}
	defer clear(runtime.Secret)
	if runtime.Connection.Mode == DatabaseModeCompose {
		if s.compose == nil {
			return errors.New("Docker Compose database testing is unavailable")
		}
		project, err := s.repo.ProjectByID(ctx, projectID)
		if err != nil {
			return err
		}
		workDir, err := databaseWorkingDirectory(project)
		if err != nil {
			return err
		}
		return s.compose.TestComposeDatabase(ctx, workDir, project.Slug, runtime.DatabaseService, runtime.Connection, runtime.Secret)
	}
	if tester, ok := s.compose.(providers.ContainerDatabaseTester); ok {
		return tester.TestDatabaseConnection(ctx, runtime.Network, runtime.Connection, runtime.Secret)
	}
	engine, ok := s.engine.(endpointDatabaseEngine)
	if !ok {
		return errors.New("database connection testing is unavailable")
	}
	connection := runtime.Connection
	if connection.Mode == DatabaseModeManaged {
		admin := engine.AdminEndpoint()
		connection.Host = admin.Host
		connection.Port = admin.Port
	}
	return engine.TestConnection(ctx, connection, runtime.Secret)
}

func (s *Service) ComposeServices(ctx context.Context, projectID string) ([]string, error) {
	if s.compose == nil {
		return nil, errors.New("Docker Compose integration is unavailable")
	}
	project, err := s.repo.ProjectByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	workDir, err := databaseWorkingDirectory(project)
	if err != nil {
		return nil, err
	}
	return s.compose.InspectComposeServices(ctx, workDir, project.Slug)
}

func (s *Service) QueueManagedMySQLAction(ctx context.Context, action string, actor, remote *string) (domain.Job, error) {
	if s.managed == nil {
		return domain.Job{}, errors.New("managed MySQL lifecycle is not configured")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "install", "start", "stop", "restart":
	default:
		return domain.Job{}, fmt.Errorf("unsupported managed MySQL action %q", action)
	}
	job, err := s.jobs.Enqueue(ctx, jobs.Request{
		Type:        JobTypeManagedMySQLAction,
		RequestedBy: actor,
		Payload:     map[string]any{"action": action},
	})
	if err != nil {
		return domain.Job{}, err
	}
	s.recordAudit(ctx, actor, "mysql."+action+".enqueue", "mysql", nil, map[string]any{"job_id": job.ID}, remote)
	return job, nil
}

func (s *Service) ensureManagedReady(ctx context.Context) error {
	if s.managed != nil {
		if err := s.managed.Ensure(ctx); err != nil {
			return fmt.Errorf("managed MySQL is unavailable: %w", err)
		}
	}
	deadline := time.Now().Add(45 * time.Second)
	var lastErr error
	for {
		if err := s.engine.Health(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("managed MySQL is unavailable: %w", lastErr)
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) decorateBinding(ctx context.Context, item DatabaseBinding) (DatabaseBinding, error) {
	item.HasSecret = item.SecretRef != ""
	connection, err := s.ResolveApplicationConnection(ctx, item.ProjectID)
	if err != nil {
		item.Status = "invalid"
		return item, nil
	}
	if connection.Mode != DatabaseModeNone {
		item.ApplicationHost = connection.Host
		item.ApplicationPort = connection.Port
		item.Status = "configured"
	}
	if item.Mode == DatabaseModeManaged && item.DatabaseID != nil {
		database, err := s.repo.DatabaseByID(ctx, *item.DatabaseID)
		if err == nil {
			item.Database = database.Name
			item.Engine = database.Engine
			item.Status = database.Status
			users, _ := s.repo.UsersByDatabase(ctx, database.ID)
			if len(users) > 0 {
				item.Username = users[0].Username
				item.HasSecret = users[0].SecretRef != ""
			}
		}
	}
	return item, nil
}

func validateBindingInput(input DatabaseBindingInput) error {
	switch input.Mode {
	case DatabaseModeNone, DatabaseModeManaged, DatabaseModeCompose, DatabaseModeExternal:
	default:
		return fmt.Errorf("invalid database mode %q", input.Mode)
	}
	if input.ApplicationService != "" && !databaseServiceName.MatchString(input.ApplicationService) {
		return errors.New("invalid Compose application service")
	}
	if input.Engine != "mysql" && input.Engine != "mariadb" {
		return errors.New("database engine must be mysql or mariadb")
	}
	if input.Port < 1 || input.Port > 65535 {
		return errors.New("database port must be between 1 and 65535")
	}
	switch input.Mode {
	case DatabaseModeCompose:
		if !databaseServiceName.MatchString(input.ComposeService) {
			return errors.New("Compose database service is not selected")
		}
		if err := ValidateIdentifier(input.Database); err != nil {
			return errors.New("invalid database name")
		}
		if err := validateUsername(input.Username); err != nil {
			return errors.New("invalid database username")
		}
	case DatabaseModeExternal:
		if err := validateExternalDatabaseHost(input.Host); err != nil {
			return err
		}
		if err := ValidateIdentifier(input.Database); err != nil {
			return errors.New("invalid database name")
		}
		if err := validateUsername(input.Username); err != nil {
			return errors.New("invalid database username")
		}
	}
	return nil
}

func validateExternalDatabaseHost(host string) error {
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "\x00\r\n /\\") {
		return errors.New("invalid external database host")
	}
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return nil
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return errors.New("invalid external database host")
		}
		for i, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') || (r == '-' && (i == 0 || i == len(label)-1)) {
				return errors.New("invalid external database host")
			}
		}
	}
	return nil
}

func databaseWorkingDirectory(project ProjectRef) (string, error) {
	root, err := filepath.Abs(project.LocalPath)
	if err != nil {
		return "", err
	}
	working := strings.TrimSpace(project.WorkingDirectory)
	if working == "" || working == "." {
		return root, nil
	}
	if filepath.IsAbs(working) {
		return "", errors.New("project working directory must be relative")
	}
	candidate := filepath.Join(root, filepath.Clean(working))
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("project working directory escapes project root")
	}
	return candidate, nil
}

func databaseEnvironment(connection providers.DatabaseConnection, password []byte) map[string]string {
	port := strconv.Itoa(connection.Port)
	secret := string(password)
	return map[string]string{
		"DB_DRIVER": "mysql", "DB_HOST": connection.Host, "DB_PORT": port,
		"DB_DATABASE": connection.Database, "DB_USERNAME": connection.Username, "DB_PASSWORD": secret,
		"DATABASE_HOST": connection.Host, "DATABASE_PORT": port, "DATABASE_NAME": connection.Database,
		"DATABASE_USER": connection.Username, "DATABASE_PASSWORD": secret,
	}
}

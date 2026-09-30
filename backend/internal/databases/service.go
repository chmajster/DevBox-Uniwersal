package databases

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/audit"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/secrets"
)

var ErrSecretsUnavailable = errors.New("database secret storage is not configured")

type databaseEngine interface {
	providers.DatabaseProvider
	Endpoint() (string, int)
	Status(context.Context) MySQLStatus
	Size(context.Context, string) (int64, error)
	DeleteUser(context.Context, string) error
	ChangePassword(context.Context, string, string) error
	RevokePrivileges(context.Context, string, string, []string) error
	DumpDatabase(context.Context, string, io.Writer) error
	RestoreDatabase(context.Context, string, io.Reader) error
}

type Service struct {
	repo       *Repository
	engine     databaseEngine
	engines    map[string]databaseEngine
	secrets    secrets.SecretStore
	jobs       jobs.JobRunner
	audit      *audit.Service
	phpMyAdmin *PHPMyAdminManager
	managed    *ManagedMySQLManager
	compose    providers.ComposeDatabaseProvider
	backupDir  string
}

type ServiceOption func(*Service)

func WithManagedMySQL(manager *ManagedMySQLManager) ServiceOption {
	return func(service *Service) { service.managed = manager }
}

func WithDatabaseEngine(engine string, provider databaseEngine) ServiceOption {
	return func(service *Service) {
		if provider == nil {
			return
		}
		if service.engines == nil {
			service.engines = make(map[string]databaseEngine)
		}
		name := normalizeDatabaseEngineName(engine)
		if name != "" {
			service.engines[name] = provider
		}
	}
}

func WithComposeDatabaseProvider(provider providers.ComposeDatabaseProvider) ServiceOption {
	return func(service *Service) { service.compose = provider }
}

func NewService(repo *Repository, engine databaseEngine, secretStore secrets.SecretStore, runner jobs.JobRunner, auditService *audit.Service, phpMyAdmin *PHPMyAdminManager, backupDir string, options ...ServiceOption) (*Service, error) {
	if repo == nil || engine == nil || runner == nil {
		return nil, errors.New("database service dependencies are incomplete")
	}
	if backupDir == "" {
		backupDir = "./data/backups/mysql"
	}
	service := &Service{
		repo:   repo,
		engine: engine,
		engines: map[string]databaseEngine{
			"mysql":   engine,
			"mariadb": engine,
		},
		secrets:    secretStore,
		jobs:       runner,
		audit:      auditService,
		phpMyAdmin: phpMyAdmin,
		backupDir:  backupDir,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	resolver := func(engineName string) (backupEngine, error) {
		return service.engineFor(engineName)
	}
	if err := runner.Register(NewBackupJobHandlerWithResolver(repo, resolver, backupDir)); err != nil {
		return nil, err
	}
	if err := runner.Register(NewRestoreJobHandlerWithResolver(repo, resolver, backupDir)); err != nil {
		return nil, err
	}
	if service.managed != nil {
		if err := runner.Register(NewManagedMySQLJobHandler(service.managed)); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func normalizeDatabaseEngineName(engine string) string {
	name := strings.ToLower(strings.TrimSpace(engine))
	if name == "postgres" {
		return "postgresql"
	}
	return name
}

func databaseAccountEngine(engine string) string {
	switch normalizeDatabaseEngineName(engine) {
	case "mysql", "mariadb":
		return "mysql"
	case "postgresql":
		return "postgresql"
	default:
		return normalizeDatabaseEngineName(engine)
	}
}

func (s *Service) engineFor(engine string) (databaseEngine, error) {
	name := normalizeDatabaseEngineName(engine)
	if name == "" {
		name = "mysql"
	}
	if provider := s.engines[name]; provider != nil {
		return provider, nil
	}
	if (name == "mysql" || name == "mariadb") && s.engine != nil {
		return s.engine, nil
	}
	return nil, fmt.Errorf("database engine %q is not configured", name)
}

func (s *Service) MySQLStatus(ctx context.Context) MySQLStatus {
	status := s.engine.Status(ctx)
	if engine, ok := s.engine.(endpointDatabaseEngine); ok {
		admin := engine.AdminEndpoint()
		application := engine.ApplicationEndpoint()
		status.AdminHost = admin.Host
		status.AdminPort = admin.Port
		status.ApplicationHost = application.Host
		status.ApplicationPort = application.Port
	}
	if s.managed != nil {
		status.Network = s.managed.Network()
	}
	return status
}

func (s *Service) ListDatabases(ctx context.Context) ([]Database, error) {
	items, err := s.repo.ListDatabases(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		engine, engineErr := s.engineFor(items[i].Engine)
		if engineErr != nil {
			continue
		}
		size, sizeErr := engine.Size(ctx, items[i].Name)
		if sizeErr != nil {
			continue
		}
		items[i].SizeBytes = &size
	}
	return items, nil
}

func (s *Service) CreateDatabase(ctx context.Context, name, engine, charset string, actor *string, remote *string) (Database, error) {
	engine = normalizeDatabaseEngineName(engine)
	if engine == "" {
		engine = "mysql"
	}
	provider, err := s.engineFor(engine)
	if err != nil {
		return Database{}, err
	}
	if charset == "" {
		if engine == "postgresql" {
			charset = "UTF8"
		} else {
			charset = "utf8mb4"
		}
	}
	if err := ValidateIdentifier(name); err != nil {
		return Database{}, err
	}
	providerName := "managed-mysql"
	if engine == "postgresql" {
		providerName = "managed-postgresql"
	}
	now := time.Now().UTC()
	item := Database{
		ID:        newID(),
		Provider:  providerName,
		Engine:    engine,
		Name:      name,
		Status:    "provisioning",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.CreateDatabase(ctx, item); err != nil {
		return Database{}, err
	}
	if err := provider.CreateDatabase(ctx, providers.DatabaseSpec{Engine: engine, Name: name, Charset: charset}); err != nil {
		_ = s.repo.DeleteDatabase(ctx, item.ID)
		return Database{}, err
	}
	if err := s.repo.UpdateDatabaseStatus(ctx, item.ID, "ready"); err != nil {
		_ = provider.DeleteDatabase(ctx, name)
		_ = s.repo.DeleteDatabase(ctx, item.ID)
		return Database{}, err
	}
	item.Status = "ready"
	s.recordAudit(ctx, actor, "database.create", "database", &item.ID, map[string]any{"name": item.Name, "engine": item.Engine}, remote)
	return item, nil
}

func (s *Service) DeleteDatabase(ctx context.Context, id string, actor *string, remote *string) error {
	item, err := s.repo.DatabaseByID(ctx, id)
	if err != nil {
		return err
	}
	provider, err := s.engineFor(item.Engine)
	if err != nil {
		return err
	}
	users, err := s.repo.UsersByDatabase(ctx, id)
	if err != nil {
		return err
	}
	for _, user := range users {
		_ = provider.Revoke(ctx, item.Name, user.Username)
		if err := s.repo.DeleteUserDatabaseGrant(ctx, user.ID, item.ID); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		refreshed, refreshErr := s.repo.UserByID(ctx, user.ID)
		if refreshErr != nil && !errors.Is(refreshErr, ErrNotFound) {
			return refreshErr
		}
		if refreshErr == nil && len(refreshed.Databases) == 0 {
			accountProvider, providerErr := s.engineFor(refreshed.Engine)
			if providerErr != nil {
				return providerErr
			}
			if err := accountProvider.DeleteUser(ctx, refreshed.Username); err != nil {
				return err
			}
			if s.secrets != nil && refreshed.SecretRef != "" {
				_ = s.secrets.Delete(ctx, "database-user", refreshed.SecretRef)
			}
			if err := s.repo.DeleteUser(ctx, refreshed.ID); err != nil {
				return err
			}
		}
	}
	if err := provider.DeleteDatabase(ctx, item.Name); err != nil {
		return err
	}
	if item.ProjectID != nil {
		if err := s.repo.DeleteDatabaseBinding(ctx, *item.ProjectID); err != nil {
			return err
		}
	}
	if err := s.repo.DeleteDatabase(ctx, id); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, "database.delete", "database", &id, map[string]any{"name": item.Name}, remote)
	return nil
}

func (s *Service) ProvisionProject(ctx context.Context, projectID, engine, charset string, actor *string, remote *string, applicationService ...string) (ProvisionResult, error) {
	if s.secrets == nil {
		return ProvisionResult{}, ErrSecretsUnavailable
	}
	if engine == "" {
		engine = "mysql"
	}
	engine = strings.ToLower(strings.TrimSpace(engine))
	if s.managed != nil && engine != "mysql" {
		return ProvisionResult{}, errors.New("managed database engine must be mysql; use Compose or external mode for MariaDB")
	}
	if charset == "" {
		charset = "utf8mb4"
	}
	if binding, err := s.GetDatabaseBinding(ctx, projectID); err == nil && binding.Mode != DatabaseModeNone && binding.Mode != DatabaseModeManaged {
		return ProvisionResult{}, fmt.Errorf("project database binding is already configured in %s mode", binding.Mode)
	}
	if existing, err := s.repo.DatabaseByProject(ctx, projectID); err == nil {
		return ProvisionResult{}, fmt.Errorf("project already has database %s", existing.Name)
	} else if !errors.Is(err, ErrNotFound) {
		return ProvisionResult{}, err
	}
	project, err := s.repo.ProjectByID(ctx, projectID)
	if err != nil {
		return ProvisionResult{}, err
	}
	if s.managed != nil {
		if err := s.ensureManagedReady(ctx); err != nil {
			return ProvisionResult{}, err
		}
	}
	dbName := projectDatabaseName(project)
	username := projectDatabaseUsername(project)
	password, err := GeneratePassword()
	if err != nil {
		return ProvisionResult{}, err
	}
	now := time.Now().UTC()
	database := Database{
		ID:              newID(),
		ProjectID:       &project.ID,
		ApplicationName: project.Name,
		Provider:        "local-mysql",
		Engine:          engine,
		Name:            dbName,
		Status:          "provisioning",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	user := DatabaseUser{
		ID:         newID(),
		Engine:     databaseAccountEngine(database.Engine),
		DatabaseID: database.ID,
		Username:   username,
		Privileges: defaultPrivileges(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	user.SecretRef = user.ID

	if err := s.repo.CreateDatabase(ctx, database); err != nil {
		return ProvisionResult{}, err
	}
	rollbackDatabase := true
	defer func() {
		if rollbackDatabase {
			_ = s.repo.DeleteDatabase(context.Background(), database.ID)
		}
	}()

	if err := s.engine.CreateDatabase(ctx, providers.DatabaseSpec{Engine: engine, Name: dbName, Owner: username, Charset: charset}); err != nil {
		return ProvisionResult{}, err
	}
	databaseCreated := true
	defer func() {
		if rollbackDatabase && databaseCreated {
			_ = s.engine.DeleteDatabase(context.Background(), dbName)
		}
	}()

	if err := s.secrets.Put(ctx, "database-user", user.SecretRef, []byte(password)); err != nil {
		return ProvisionResult{}, fmt.Errorf("store database credential: %w", err)
	}
	secretCreated := true
	defer func() {
		if rollbackDatabase && secretCreated {
			_ = s.secrets.Delete(context.Background(), "database-user", user.SecretRef)
		}
	}()

	if err := s.engine.CreateUser(ctx, username, user.SecretRef); err != nil {
		return ProvisionResult{}, err
	}
	userCreated := true
	defer func() {
		if rollbackDatabase && userCreated {
			_ = s.engine.DeleteUser(context.Background(), username)
		}
	}()

	if err := s.engine.Grant(ctx, dbName, username, user.Privileges); err != nil {
		return ProvisionResult{}, err
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		return ProvisionResult{}, err
	}
	if err := s.repo.UpdateDatabaseStatus(ctx, database.ID, "ready"); err != nil {
		return ProvisionResult{}, err
	}
	bindingInput := DatabaseBindingInput{Mode: DatabaseModeManaged, Engine: engine, Port: 3306}
	if len(applicationService) > 0 {
		bindingInput.ApplicationService = strings.TrimSpace(applicationService[0])
	}
	if _, err := s.UpdateDatabaseBinding(ctx, projectID, bindingInput, actor, remote); err != nil {
		return ProvisionResult{}, err
	}
	rollbackDatabase = false
	database.Status = "ready"
	database.Username = username
	provider, err := s.engineFor(database.Engine)
	if err != nil {
		return ProvisionResult{}, err
	}
	host, port := provider.Endpoint()
	if endpointEngine, ok := provider.(endpointDatabaseEngine); ok {
		endpoint := endpointEngine.ApplicationEndpoint()
		host, port = endpoint.Host, endpoint.Port
	}
	s.recordAudit(ctx, actor, "database.provision", "project", &project.ID, map[string]any{"database_id": database.ID, "database": dbName, "username": username}, remote)
	return ProvisionResult{
		Database: database,
		Credential: ConnectionConfig{
			Engine: engine, Host: host, Port: port, Database: dbName, Username: username, Password: password,
		},
	}, nil
}

func (s *Service) ListUsers(ctx context.Context) ([]DatabaseUser, error) {
	return s.repo.ListUsers(ctx)
}

func (s *Service) GetUser(ctx context.Context, id string) (DatabaseUser, error) {
	return s.repo.UserByID(ctx, id)
}

func (s *Service) CreateUser(ctx context.Context, databaseID, username string, requestedPassword *string, privileges []string, actor *string, remote *string) (DatabaseUser, string, error) {
	if s.secrets == nil {
		return DatabaseUser{}, "", ErrSecretsUnavailable
	}
	database, err := s.repo.DatabaseByID(ctx, databaseID)
	if err != nil {
		return DatabaseUser{}, "", err
	}
	if username == "" {
		username = generatedUsername(database.Name, database.ID)
	}
	if err := validateUsername(username); err != nil {
		return DatabaseUser{}, "", err
	}
	engineName := databaseAccountEngine(database.Engine)
	if _, err := s.repo.UserByEngineUsername(ctx, engineName, username); err == nil {
		return DatabaseUser{}, "", fmt.Errorf("%w: %s", ErrDatabaseUserExists, username)
	} else if !errors.Is(err, ErrNotFound) {
		return DatabaseUser{}, "", err
	}
	if len(privileges) == 0 {
		privileges = defaultPrivilegesForEngine(database.Engine)
	}
	privileges, err = normalizePrivileges(privileges)
	if err != nil {
		return DatabaseUser{}, "", err
	}
	password, err := resolveRequestedDatabasePassword(requestedPassword)
	if err != nil {
		return DatabaseUser{}, "", err
	}
	now := time.Now().UTC()
	user := DatabaseUser{ID: newID(), Engine: engineName, DatabaseID: database.ID, Username: username, SecretRef: "", Privileges: privileges, CreatedAt: now, UpdatedAt: now}
	user.SecretRef = user.ID
	if err := s.secrets.Put(ctx, "database-user", user.SecretRef, []byte(password)); err != nil {
		return DatabaseUser{}, "", err
	}
	provider, err := s.engineFor(database.Engine)
	if err != nil {
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
		return DatabaseUser{}, "", err
	}
	if err := provider.CreateUser(ctx, username, user.SecretRef); err != nil {
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
		return DatabaseUser{}, "", err
	}
	if err := provider.Grant(ctx, database.Name, username, privileges); err != nil {
		_ = provider.DeleteUser(ctx, username)
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
		return DatabaseUser{}, "", err
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		_ = provider.Revoke(ctx, database.Name, username)
		_ = provider.DeleteUser(ctx, username)
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
		return DatabaseUser{}, "", err
	}
	created, err := s.repo.UserByID(ctx, user.ID)
	if err != nil {
		return DatabaseUser{}, "", err
	}
	s.recordAudit(ctx, actor, "database_user.create", "database_user", &user.ID, map[string]any{"database_id": database.ID, "username": username}, remote)
	return created, password, nil
}

func (s *Service) DatabaseUserConnection(ctx context.Context, userID, password string) (ConnectionConfig, error) {
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return ConnectionConfig{}, err
	}
	provider, err := s.engineFor(user.Engine)
	if err != nil {
		return ConnectionConfig{}, err
	}
	host, port := provider.Endpoint()
	if endpointEngine, ok := provider.(endpointDatabaseEngine); ok {
		endpoint := endpointEngine.ApplicationEndpoint()
		host, port = endpoint.Host, endpoint.Port
	}
	connection := ConnectionConfig{Engine: user.Engine, Host: host, Port: port, Username: user.Username, Password: password}
	if len(user.Databases) > 0 {
		connection.Database = user.Databases[0].DatabaseName
		connection.Engine = user.Databases[0].Engine
	}
	return connection, nil
}

func (s *Service) DeleteUser(ctx context.Context, id string, actor *string, remote *string) error {
	user, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return err
	}
	for _, grant := range user.Databases {
		database, dbErr := s.repo.DatabaseByID(ctx, grant.DatabaseID)
		if dbErr != nil {
			return dbErr
		}
		provider, providerErr := s.engineFor(database.Engine)
		if providerErr != nil {
			return providerErr
		}
		_ = provider.Revoke(ctx, database.Name, user.Username)
	}
	provider, err := s.engineFor(user.Engine)
	if err != nil {
		return err
	}
	if err := provider.DeleteUser(ctx, user.Username); err != nil {
		return err
	}
	if s.secrets != nil && user.SecretRef != "" {
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
	}
	if err := s.repo.DeleteUser(ctx, id); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, "database_user.delete", "database_user", &id, map[string]any{"username": user.Username}, remote)
	return nil
}

func resolveRequestedDatabasePassword(requested *string) (string, error) {
	if requested == nil {
		return GeneratePassword()
	}
	password := *requested
	if password == "" {
		return "", nil
	}
	if len(password) < 8 || len(password) > 256 {
		return "", errors.New("database user password must be empty or contain between 8 and 256 characters")
	}
	return password, nil
}

func (s *Service) ChangeUserPassword(ctx context.Context, id string, requestedPassword *string, actor *string, remote *string) (string, error) {
	if s.secrets == nil {
		return "", ErrSecretsUnavailable
	}
	user, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return "", err
	}
	old, err := s.secrets.Get(ctx, "database-user", user.SecretRef)
	if err != nil {
		return "", err
	}
	password, err := resolveRequestedDatabasePassword(requestedPassword)
	if err != nil {
		return "", err
	}
	provider, err := s.engineFor(user.Engine)
	if err != nil {
		return "", err
	}
	if err := provider.ChangePassword(ctx, user.Username, password); err != nil {
		return "", err
	}
	if err := s.secrets.Put(ctx, "database-user", user.SecretRef, []byte(password)); err != nil {
		_ = provider.ChangePassword(context.Background(), user.Username, string(old))
		return "", fmt.Errorf("persist changed database password: %w", err)
	}
	s.recordAudit(ctx, actor, "database_user.password_change", "database_user", &id, map[string]any{"username": user.Username}, remote)
	return password, nil
}

func (s *Service) ChangeGrants(ctx context.Context, id, databaseID, action string, privileges []string, actor *string, remote *string) (DatabaseUser, error) {
	user, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return DatabaseUser{}, err
	}
	if databaseID == "" {
		databaseID = user.DatabaseID
	}
	if databaseID == "" {
		return DatabaseUser{}, errors.New("database is not selected")
	}
	database, err := s.repo.DatabaseByID(ctx, databaseID)
	if err != nil {
		return DatabaseUser{}, err
	}
	if databaseAccountEngine(database.Engine) != databaseAccountEngine(user.Engine) {
		return DatabaseUser{}, errors.New("database belongs to a different SQL server")
	}
	privileges, err = normalizePrivileges(privileges)
	if err != nil {
		return DatabaseUser{}, err
	}
	provider, err := s.engineFor(database.Engine)
	if err != nil {
		return DatabaseUser{}, err
	}
	current := []string{}
	for _, grant := range user.Databases {
		if grant.DatabaseID == database.ID {
			current = append(current, grant.Privileges...)
			break
		}
	}
	switch strings.ToLower(action) {
	case "grant":
		if err := provider.Grant(ctx, database.Name, user.Username, privileges); err != nil {
			return DatabaseUser{}, err
		}
		for _, privilege := range privileges {
			if !slices.Contains(current, privilege) {
				current = append(current, privilege)
			}
		}
	case "revoke":
		filtered := current[:0]
		for _, existing := range current {
			if !slices.Contains(privileges, existing) {
				filtered = append(filtered, existing)
			}
		}
		if len(filtered) == 0 && databaseAccountEngine(database.Engine) == "postgresql" {
			if err := provider.Revoke(ctx, database.Name, user.Username); err != nil {
				return DatabaseUser{}, err
			}
		} else if err := provider.RevokePrivileges(ctx, database.Name, user.Username, privileges); err != nil {
			return DatabaseUser{}, err
		}
		current = filtered
	default:
		return DatabaseUser{}, errors.New("grant action must be grant or revoke")
	}
	if len(current) == 0 {
		if err := s.repo.DeleteUserDatabaseGrant(ctx, user.ID, database.ID); err != nil && !errors.Is(err, ErrNotFound) {
			return DatabaseUser{}, err
		}
	} else if err := s.repo.UpsertUserDatabaseGrant(ctx, user.ID, database.ID, current); err != nil {
		return DatabaseUser{}, err
	}
	s.recordAudit(ctx, actor, "database_user."+strings.ToLower(action), "database_user", &id, map[string]any{"database_id": database.ID, "privileges": privileges}, remote)
	return s.repo.UserByID(ctx, user.ID)
}

func (s *Service) SetUserDatabaseAccess(ctx context.Context, userID, databaseID string, privileges []string, actor *string, remote *string) (DatabaseUser, error) {
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return DatabaseUser{}, err
	}
	database, err := s.repo.DatabaseByID(ctx, databaseID)
	if err != nil {
		return DatabaseUser{}, err
	}
	if databaseAccountEngine(database.Engine) != databaseAccountEngine(user.Engine) {
		return DatabaseUser{}, errors.New("database belongs to a different SQL server")
	}
	if len(privileges) == 0 {
		return DatabaseUser{}, errors.New("at least one database privilege is required")
	}
	privileges, err = normalizePrivileges(privileges)
	if err != nil {
		return DatabaseUser{}, err
	}
	provider, err := s.engineFor(database.Engine)
	if err != nil {
		return DatabaseUser{}, err
	}
	var old []string
	for _, grant := range user.Databases {
		if grant.DatabaseID == database.ID {
			old = grant.Privileges
			break
		}
	}
	toGrant := make([]string, 0)
	for _, privilege := range privileges {
		if !slices.Contains(old, privilege) {
			toGrant = append(toGrant, privilege)
		}
	}
	toRevoke := make([]string, 0)
	for _, privilege := range old {
		if !slices.Contains(privileges, privilege) {
			toRevoke = append(toRevoke, privilege)
		}
	}
	if len(toGrant) > 0 {
		if err := provider.Grant(ctx, database.Name, user.Username, toGrant); err != nil {
			return DatabaseUser{}, err
		}
	}
	if len(toRevoke) > 0 {
		if err := provider.RevokePrivileges(ctx, database.Name, user.Username, toRevoke); err != nil {
			return DatabaseUser{}, err
		}
	}
	if err := s.repo.UpsertUserDatabaseGrant(ctx, user.ID, database.ID, privileges); err != nil {
		return DatabaseUser{}, err
	}
	s.recordAudit(ctx, actor, "database_user.database_access.update", "database_user", &user.ID, map[string]any{"database_id": database.ID, "privileges": privileges}, remote)
	return s.repo.UserByID(ctx, user.ID)
}

func (s *Service) RemoveUserDatabaseAccess(ctx context.Context, userID, databaseID string, actor *string, remote *string) (DatabaseUser, error) {
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return DatabaseUser{}, err
	}
	var grant *DatabaseUserGrant
	for i := range user.Databases {
		if user.Databases[i].DatabaseID == databaseID {
			copy := user.Databases[i]
			grant = &copy
			break
		}
	}
	if grant == nil {
		return DatabaseUser{}, ErrNotFound
	}
	database, err := s.repo.DatabaseByID(ctx, databaseID)
	if err != nil {
		return DatabaseUser{}, err
	}
	provider, err := s.engineFor(database.Engine)
	if err != nil {
		return DatabaseUser{}, err
	}
	if len(grant.Privileges) > 0 {
		if databaseAccountEngine(database.Engine) == "postgresql" {
			if err := provider.Revoke(ctx, database.Name, user.Username); err != nil {
				return DatabaseUser{}, err
			}
		} else if err := provider.RevokePrivileges(ctx, database.Name, user.Username, grant.Privileges); err != nil {
			return DatabaseUser{}, err
		}
	}
	if err := s.repo.DeleteUserDatabaseGrant(ctx, user.ID, database.ID); err != nil {
		return DatabaseUser{}, err
	}
	s.recordAudit(ctx, actor, "database_user.database_access.delete", "database_user", &user.ID, map[string]any{"database_id": database.ID}, remote)
	return s.repo.UserByID(ctx, user.ID)
}

func defaultPrivilegesForEngine(engine string) []string {
	if normalizeDatabaseEngineName(engine) == "postgresql" {
		return []string{"SELECT", "INSERT", "UPDATE", "DELETE", "CREATE", "REFERENCES", "TRIGGER", "EXECUTE"}
	}
	return defaultPrivileges()
}

func (s *Service) QueueBackup(ctx context.Context, databaseID string, actor *string, remote *string) (domain.Job, Backup, error) {
	database, err := s.repo.DatabaseByID(ctx, databaseID)
	if err != nil {
		return domain.Job{}, Backup{}, err
	}
	now := time.Now().UTC()
	backup := Backup{
		ID:         newID(),
		DatabaseID: database.ID,
		FileName:   fmt.Sprintf("%s-%s-%s.sql", database.Name, now.Format("20060102T150405Z"), shortHash(newID())),
		Status:     "queued",
		CreatedAt:  now,
	}
	if err := s.repo.CreateBackup(ctx, backup); err != nil {
		return domain.Job{}, Backup{}, err
	}
	job, err := s.jobs.Enqueue(ctx, jobs.Request{Type: JobTypeDatabaseBackup, ProjectID: database.ProjectID, RequestedBy: actor, Payload: map[string]any{"database_id": database.ID, "backup_id": backup.ID}})
	if err != nil {
		_ = s.repo.DeleteBackup(ctx, backup.ID)
		return domain.Job{}, Backup{}, err
	}
	s.recordAudit(ctx, actor, "database.backup.enqueue", "database", &database.ID, map[string]any{"backup_id": backup.ID, "job_id": job.ID}, remote)
	return job, backup, nil
}

func (s *Service) QueueRestore(ctx context.Context, databaseID, backupID string, actor *string, remote *string) (domain.Job, error) {
	database, err := s.repo.DatabaseByID(ctx, databaseID)
	if err != nil {
		return domain.Job{}, err
	}
	backup, err := s.repo.BackupByID(ctx, backupID)
	if err != nil {
		return domain.Job{}, err
	}
	if backup.DatabaseID != database.ID || backup.Status != "ready" {
		return domain.Job{}, errors.New("backup is not restorable for this database")
	}
	job, err := s.jobs.Enqueue(ctx, jobs.Request{Type: JobTypeDatabaseRestore, ProjectID: database.ProjectID, RequestedBy: actor, Payload: map[string]any{"database_id": database.ID, "backup_id": backup.ID}})
	if err != nil {
		return domain.Job{}, err
	}
	s.recordAudit(ctx, actor, "database.restore.enqueue", "database", &database.ID, map[string]any{"backup_id": backup.ID, "job_id": job.ID}, remote)
	return job, nil
}

func (s *Service) ListBackups(ctx context.Context, databaseID string) ([]Backup, error) {
	if _, err := s.repo.DatabaseByID(ctx, databaseID); err != nil {
		return nil, err
	}
	return s.repo.ListBackups(ctx, databaseID)
}

func (s *Service) BackupDownload(ctx context.Context, backupID string) (Backup, string, error) {
	backup, err := s.repo.BackupByID(ctx, backupID)
	if err != nil {
		return Backup{}, "", err
	}
	if backup.Status != "ready" {
		return Backup{}, "", errors.New("backup is not ready")
	}
	path, err := backupPath(s.backupDir, backup.FileName)
	if err != nil {
		return Backup{}, "", err
	}
	if _, err := os.Stat(path); err != nil {
		return Backup{}, "", fmt.Errorf("backup file unavailable: %w", err)
	}
	return backup, path, nil
}

func (s *Service) DeleteBackup(ctx context.Context, backupID string, actor *string, remote *string) error {
	backup, err := s.repo.BackupByID(ctx, backupID)
	if err != nil {
		return err
	}
	path, err := backupPath(s.backupDir, backup.FileName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete backup file: %w", err)
	}
	if err := s.repo.DeleteBackup(ctx, backupID); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, "database.backup.delete", "database_backup", &backupID, map[string]any{"database_id": backup.DatabaseID}, remote)
	return nil
}

func (s *Service) PHPMyAdminStatus(ctx context.Context) (PHPMyAdminStatus, error) {
	if s.phpMyAdmin == nil {
		return PHPMyAdminStatus{State: "disabled"}, nil
	}
	return s.phpMyAdmin.Status(ctx)
}

func (s *Service) PHPMyAdminAction(ctx context.Context, action string, actor *string, remote *string) (PHPMyAdminStatus, error) {
	if s.phpMyAdmin == nil {
		return PHPMyAdminStatus{}, errors.New("phpMyAdmin manager is not configured")
	}
	if (action == "install" || action == "start" || action == "restart") && s.managed != nil {
		if err := s.ensureManagedReady(ctx); err != nil {
			return PHPMyAdminStatus{}, err
		}
	}
	var (
		status PHPMyAdminStatus
		err    error
	)
	switch action {
	case "install":
		status, err = s.phpMyAdmin.Install(ctx)
	case "start":
		if _, err = s.phpMyAdmin.Install(ctx); err == nil {
			status, err = s.phpMyAdmin.Start(ctx)
		}
	case "stop":
		status, err = s.phpMyAdmin.Stop(ctx)
	case "restart":
		if _, err = s.phpMyAdmin.Install(ctx); err == nil {
			status, err = s.phpMyAdmin.Restart(ctx)
		}
	case "uninstall":
		status, err = s.phpMyAdmin.Uninstall(ctx)
	default:
		return PHPMyAdminStatus{}, errors.New("unsupported phpMyAdmin action")
	}
	if err == nil {
		s.recordAudit(ctx, actor, "phpmyadmin."+action, "phpmyadmin", nil, map[string]any{"state": status.State}, remote)
	}
	return status, err
}

func (s *Service) recordAudit(ctx context.Context, actor *string, action, resourceType string, resourceID *string, metadata map[string]any, remote *string) {
	if s.audit != nil {
		_ = s.audit.Record(ctx, actor, action, resourceType, resourceID, metadata, remote)
	}
}

func projectDatabaseName(project ProjectRef) string {
	base := safeNamePart(project.Slug)
	if base == "" {
		base = "app"
	}
	if len(base) > 48 {
		base = base[:48]
	}
	return "dbx_" + base + "_" + shortHash(project.ID)
}

func projectDatabaseUsername(project ProjectRef) string {
	base := safeNamePart(project.Slug)
	if base == "" {
		base = "app"
	}
	if len(base) > 20 {
		base = base[:20]
	}
	return "dbx_" + base + "_" + shortHash(project.ID)
}

func generatedUsername(databaseName, databaseID string) string {
	base := safeNamePart(databaseName)
	if len(base) > 20 {
		base = base[:20]
	}
	return "dbx_" + base + "_" + shortHash(databaseID)
}

package databases

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
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
	bindingMu  sync.Mutex
	repo       *Repository
	engine     databaseEngine
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
		repo:       repo,
		engine:     engine,
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
	if err := runner.Register(NewBackupJobHandler(repo, engine, backupDir)); err != nil {
		return nil, err
	}
	if err := runner.Register(NewRestoreJobHandler(repo, engine, backupDir)); err != nil {
		return nil, err
	}
	if err := runner.Register(&NetworkTestHandler{service: service}); err != nil {
		return nil, err
	}
	if err := runner.Register(&PHPMyAdminJobHandler{service: service}); err != nil {
		return nil, err
	}
	if service.managed != nil {
		if err := runner.Register(NewManagedMySQLJobHandler(service.managed)); err != nil {
			return nil, err
		}
	}
	return service, nil
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
		size, sizeErr := s.engine.Size(ctx, items[i].Name)
		if sizeErr != nil {
			continue
		}
		items[i].SizeBytes = &size
	}
	return items, nil
}

func (s *Service) CreateDatabase(ctx context.Context, name, engine, charset string, actor *string, remote *string) (Database, error) {
	if engine == "" {
		engine = "mysql"
	}
	if charset == "" {
		charset = "utf8mb4"
	}
	if err := ValidateIdentifier(name); err != nil {
		return Database{}, err
	}
	now := time.Now().UTC()
	item := Database{
		ID:        newID(),
		Provider:  "local-mysql",
		Engine:    engine,
		Name:      name,
		Status:    "provisioning",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.CreateDatabase(ctx, item); err != nil {
		return Database{}, err
	}
	if err := s.engine.CreateDatabase(ctx, providers.DatabaseSpec{Engine: engine, Name: name, Charset: charset}); err != nil {
		_ = s.repo.DeleteDatabase(ctx, item.ID)
		return Database{}, err
	}
	if err := s.repo.UpdateDatabaseStatus(ctx, item.ID, "ready"); err != nil {
		_ = s.engine.DeleteDatabase(ctx, name)
		_ = s.repo.DeleteDatabase(ctx, item.ID)
		return Database{}, err
	}
	item.Status = "ready"
	s.recordAudit(ctx, actor, "database.create", "database", &item.ID, map[string]any{"name": item.Name, "engine": item.Engine}, remote)
	return item, nil
}

func (s *Service) DeleteDatabase(ctx context.Context, id string, actor *string, remote *string) error {
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	item, err := s.repo.DatabaseByID(ctx, id)
	if err != nil {
		return err
	}
	users, err := s.repo.UsersByDatabase(ctx, id)
	if err != nil {
		return err
	}
	for _, user := range users {
		_ = s.engine.Revoke(ctx, item.Name, user.Username)
		if err := s.engine.DeleteUser(ctx, user.Username); err != nil {
			return err
		}
		if s.secrets != nil && user.SecretRef != "" {
			_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
		}
	}
	if err := s.engine.DeleteDatabase(ctx, item.Name); err != nil {
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
	bindingInput := DatabaseBindingInput{Mode: DatabaseModeManaged, Engine: engine, Port: 3306, DatabaseUserID: user.ID}
	if len(applicationService) > 0 {
		bindingInput.ApplicationService = strings.TrimSpace(applicationService[0])
	}
	if _, err := s.UpdateDatabaseBinding(ctx, projectID, bindingInput, actor, remote); err != nil {
		return ProvisionResult{}, err
	}
	rollbackDatabase = false
	database.Status = "ready"
	database.Username = username
	host, port := s.engine.Endpoint()
	if endpointEngine, ok := s.engine.(endpointDatabaseEngine); ok {
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
	if len(privileges) == 0 {
		privileges = defaultPrivileges()
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
	user := DatabaseUser{ID: newID(), DatabaseID: database.ID, Username: username, SecretRef: "", Privileges: privileges, CreatedAt: now, UpdatedAt: now}
	user.SecretRef = user.ID
	if err := s.secrets.Put(ctx, "database-user", user.SecretRef, []byte(password)); err != nil {
		return DatabaseUser{}, "", err
	}
	if err := s.engine.CreateUser(ctx, username, user.SecretRef); err != nil {
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
		return DatabaseUser{}, "", err
	}
	if err := s.engine.Grant(ctx, database.Name, username, privileges); err != nil {
		_ = s.engine.DeleteUser(ctx, username)
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
		return DatabaseUser{}, "", err
	}
	if err := s.repo.CreateUser(ctx, user); err != nil {
		_ = s.engine.Revoke(ctx, database.Name, username)
		_ = s.engine.DeleteUser(ctx, username)
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
		return DatabaseUser{}, "", err
	}
	s.recordAudit(ctx, actor, "database_user.create", "database_user", &user.ID, map[string]any{"database_id": database.ID, "username": username}, remote)
	return user, password, nil
}

func (s *Service) DatabaseUserConnection(ctx context.Context, userID, password string) (ConnectionConfig, error) {
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return ConnectionConfig{}, err
	}
	database, err := s.repo.DatabaseByID(ctx, user.DatabaseID)
	if err != nil {
		return ConnectionConfig{}, err
	}
	host, port := s.engine.Endpoint()
	if endpointEngine, ok := s.engine.(endpointDatabaseEngine); ok {
		endpoint := endpointEngine.ApplicationEndpoint()
		host, port = endpoint.Host, endpoint.Port
	}
	return ConnectionConfig{
		Engine: database.Engine, Host: host, Port: port, Database: database.Name,
		Username: user.Username, Password: password,
	}, nil
}

func (s *Service) DeleteUser(ctx context.Context, id string, actor *string, remote *string) error {
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	inUse, err := s.repo.DatabaseUserInUse(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return ErrDatabaseUserInUse
	}
	user, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return err
	}
	database, err := s.repo.DatabaseByID(ctx, user.DatabaseID)
	if err != nil {
		return err
	}
	_ = s.engine.Revoke(ctx, database.Name, user.Username)
	if err := s.engine.DeleteUser(ctx, user.Username); err != nil {
		return err
	}
	if s.secrets != nil && user.SecretRef != "" {
		_ = s.secrets.Delete(ctx, "database-user", user.SecretRef)
	}
	if err := s.repo.DeleteUser(ctx, id); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, "database_user.delete", "database_user", &id, map[string]any{"database_id": database.ID, "username": user.Username}, remote)
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
	if err := s.engine.ChangePassword(ctx, user.Username, password); err != nil {
		return "", err
	}
	if err := s.secrets.Put(ctx, "database-user", user.SecretRef, []byte(password)); err != nil {
		_ = s.engine.ChangePassword(context.Background(), user.Username, string(old))
		return "", fmt.Errorf("persist changed database password: %w", err)
	}
	s.recordAudit(ctx, actor, "database_user.password_change", "database_user", &id, map[string]any{"username": user.Username}, remote)
	return password, nil
}

func (s *Service) ChangeGrants(ctx context.Context, id, action string, privileges []string, actor *string, remote *string) (DatabaseUser, error) {
	user, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return DatabaseUser{}, err
	}
	database, err := s.repo.DatabaseByID(ctx, user.DatabaseID)
	if err != nil {
		return DatabaseUser{}, err
	}
	privileges, err = normalizePrivileges(privileges)
	if err != nil {
		return DatabaseUser{}, err
	}
	switch strings.ToLower(action) {
	case "grant":
		if err := s.engine.Grant(ctx, database.Name, user.Username, privileges); err != nil {
			return DatabaseUser{}, err
		}
		for _, privilege := range privileges {
			if !slices.Contains(user.Privileges, privilege) {
				user.Privileges = append(user.Privileges, privilege)
			}
		}
	case "revoke":
		if err := s.engine.RevokePrivileges(ctx, database.Name, user.Username, privileges); err != nil {
			return DatabaseUser{}, err
		}
		filtered := user.Privileges[:0]
		for _, current := range user.Privileges {
			if !slices.Contains(privileges, current) {
				filtered = append(filtered, current)
			}
		}
		user.Privileges = filtered
	default:
		return DatabaseUser{}, errors.New("grant action must be grant or revoke")
	}
	if err := s.repo.UpdateUserPrivileges(ctx, user.ID, user.Privileges); err != nil {
		return DatabaseUser{}, err
	}
	s.recordAudit(ctx, actor, "database_user."+strings.ToLower(action), "database_user", &id, map[string]any{"database_id": database.ID, "privileges": privileges}, remote)
	return user, nil
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
	if action != "stop" && s.managed != nil {
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

package databases

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controldb "github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

func databaseBindingTestService(t *testing.T) (*Service, *Repository, *fakeSecretStore, *MySQLProvider) {
	t.Helper()
	db, err := controldb.Open(filepath.Join(t.TempDir(), "devbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations := filepath.Join("..", "..", "..", "migrations")
	if _, err := os.Stat(migrations); err != nil {
		t.Fatalf("migrations path unavailable: %v", err)
	}
	if err := controldb.Migrate(context.Background(), db, migrations); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`INSERT INTO projects(
		id,name,slug,description,status,work_dir,created_at,updated_at,source_type,local_path,
		runtime,runtime_version,container_policy,deployment_mode,working_directory,
		build_command,start_command,healthcheck,auto_start,current_commit
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"project-1", "Plan", "plan", "", "ready", t.TempDir(), now, now, "local", t.TempDir(),
		"php", "8.3", "auto", "docker", "", "", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	store := &fakeSecretStore{values: map[string][]byte{}}
	engine := NewMySQLProvider(MySQLConfig{
		Host:                    "127.0.0.1",
		Port:                    3306,
		AdminUser:               "root",
		ApplicationEndpointHost: DefaultManagedMySQLContainer,
		ApplicationEndpointPort: 3306,
		ApplicationHost:         "%",
	}, store)
	service := &Service{repo: repo, engine: engine, secrets: store}
	return service, repo, store, engine
}

func TestManagedDatabaseResolverUsesContainerDNS(t *testing.T) {
	ctx := context.Background()
	service, repo, store, engine := databaseBindingTestService(t)
	now := time.Now().UTC()
	projectID := "project-1"
	database := Database{
		ID: "database-1", ProjectID: &projectID, ApplicationName: "Plan", Provider: "local-mysql",
		Engine: "mysql", Name: "dbx_plan_12345678", Status: "ready", CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateDatabase(ctx, database); err != nil {
		t.Fatal(err)
	}
	user := DatabaseUser{
		ID: "user-1", DatabaseID: database.ID, Username: "dbx_plan_12345678",
		SecretRef: "user-1", Privileges: defaultPrivileges(), CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Put(ctx, "database-user", user.SecretRef, []byte("managed-test-secret")); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	binding, err := service.UpdateDatabaseBinding(ctx, projectID, DatabaseBindingInput{
		Mode: DatabaseModeManaged, Engine: "mysql", Port: 3306,
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if binding.ApplicationHost != DefaultManagedMySQLContainer || binding.ApplicationPort != 3306 {
		t.Fatalf("unexpected application endpoint: %s:%d", binding.ApplicationHost, binding.ApplicationPort)
	}
	connection, err := service.ResolveApplicationConnection(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if connection.Host != "devbox-mysql" || connection.Port != 3306 {
		t.Fatalf("expected devbox-mysql:3306, got %s:%d", connection.Host, connection.Port)
	}
	if connection.Database != database.Name || connection.Username != user.Username {
		t.Fatalf("unexpected managed connection: %+v", connection)
	}
	admin := engine.AdminEndpoint()
	application := engine.ApplicationEndpoint()
	if admin == application {
		t.Fatalf("admin and application endpoints must differ: %+v", admin)
	}
	if admin.Host != "127.0.0.1" || application.Host != "devbox-mysql" {
		t.Fatalf("unexpected endpoint separation: admin=%+v application=%+v", admin, application)
	}
}

func TestExternalDatabaseBindingKeepsPasswordOnlyInSecretStore(t *testing.T) {
	ctx := context.Background()
	service, repo, store, _ := databaseBindingTestService(t)
	item, err := service.UpdateDatabaseBinding(ctx, "project-1", DatabaseBindingInput{
		Mode: DatabaseModeExternal, Engine: "mysql", Host: "mysql.example.internal", Port: 3307,
		Database: "plan", Username: "plan_user", Password: "external-test-secret",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if item.HasSecret != true {
		t.Fatal("expected binding to report a stored secret")
	}
	persisted, err := repo.DatabaseBindingByProject(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.SecretRef == "" || persisted.SecretRef == "external-test-secret" {
		t.Fatalf("expected opaque secret reference, got %q", persisted.SecretRef)
	}
	if got := string(store.values[bindingSecretScope("project-1")+"/"+persisted.SecretRef]); got != "external-test-secret" {
		t.Fatalf("unexpected SecretStore value: %q", got)
	}
	connection, err := service.ResolveApplicationConnection(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if connection.Mode != providers.DatabaseModeExternal || connection.Host != "mysql.example.internal" || connection.Port != 3307 {
		t.Fatalf("unexpected external connection: %+v", connection)
	}
	runtime, err := service.ResolveRuntimeDatabase(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(runtime.Secret)
	if string(runtime.Secret) != "external-test-secret" {
		t.Fatal("runtime secret was not resolved from SecretStore")
	}
	body, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "external-test-secret") || strings.Contains(string(body), persisted.SecretRef) {
		t.Fatalf("secret leaked through binding JSON: %s", body)
	}
}

func TestComposeDatabaseResolverUsesComposeDNS(t *testing.T) {
	ctx := context.Background()
	service, _, _, _ := databaseBindingTestService(t)
	if _, err := service.UpdateDatabaseBinding(ctx, "project-1", DatabaseBindingInput{
		Mode: DatabaseModeCompose, Engine: "mysql", ApplicationService: "web", ComposeService: "db",
		Port: 3306, Database: "plan", Username: "plan_user", Password: "compose-test-secret",
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	connection, err := service.ResolveApplicationConnection(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if connection.Host != "db" || connection.Port != 3306 || connection.Mode != providers.DatabaseModeCompose {
		t.Fatalf("expected Compose DNS db:3306, got %+v", connection)
	}
}

func TestDatabaseBindingRejectsInvalidExternalConfiguration(t *testing.T) {
	service, _, _, _ := databaseBindingTestService(t)
	_, err := service.UpdateDatabaseBinding(context.Background(), "project-1", DatabaseBindingInput{
		Mode: DatabaseModeExternal, Engine: "mysql", Host: "bad host/with/path", Port: 3306,
		Database: "plan", Username: "plan_user", Password: "secret",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected invalid external host to be rejected")
	}
}

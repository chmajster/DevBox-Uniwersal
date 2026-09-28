package projects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/containerspec"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
)

func TestDeploymentStateMachine(t *testing.T) {
	states := []string{DeploymentQueued, DeploymentPreparing, DeploymentUpdatingSource, DeploymentDatabase, DeploymentDependencies, DeploymentBuilding, DeploymentStarting, DeploymentHealthcheck, DeploymentSuccess}
	for i := 0; i < len(states)-1; i++ {
		if !validDeploymentTransition(states[i], states[i+1]) {
			t.Fatalf("expected valid transition %s -> %s", states[i], states[i+1])
		}
	}
	if validDeploymentTransition(DeploymentPreparing, DeploymentSuccess) {
		t.Fatal("unexpected skipped transition")
	}
}

func TestDeploymentFailurePersistsProviderUnavailable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations := filepath.Join("..", "..", "..", "migrations")
	if _, err := os.Stat(migrations); err != nil {
		t.Fatalf("migrations path unavailable: %v", err)
	}
	if err := database.Migrate(context.Background(), db, migrations); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	now := time.Now().UTC()
	project := Project{ID: NewID(), Name: "failure-test", Slug: "failure-test", Status: "ready", SourceType: SourceLocal, LocalPath: t.TempDir(), Runtime: "static", ContainerPolicy: ContainerPolicyAuto, CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(context.Background(), project, ""); err != nil {
		t.Fatal(err)
	}
	deployment := Deployment{ID: NewID(), ProjectID: project.ID, Status: DeploymentQueued, Stage: DeploymentQueued, CreatedAt: now}
	if err := repo.CreateDeployment(context.Background(), deployment); err != nil {
		t.Fatal(err)
	}
	handler := NewDeploymentHandler(repo, NewGitClient(nil), runtimes.NewRegistry(), &testJobLogger{})
	_, err = handler.Run(context.Background(), domain.Job{ID: "job-test", Payload: map[string]any{"project_id": project.ID, "deployment_id": deployment.ID}})
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("expected provider unavailable error, got %v", err)
	}
	items, err := repo.ListDeployments(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != DeploymentFailed || items[0].Stage != DeploymentDatabase || !strings.Contains(items[0].Error, "provider unavailable") {
		t.Fatalf("failure state not persisted: %+v", items)
	}
}

type testJobLogger struct{}

func (l *testJobLogger) Log(context.Context, string, string, string, map[string]any) error {
	return nil
}

func TestProjectDatabaseEnvironmentInjectsReservedVariablesAndAliases(t *testing.T) {
	runtime := providers.ProjectDatabaseRuntime{
		Connection: providers.DatabaseConnection{
			Engine: "mysql", Host: "devbox-mysql", Port: 3306, Database: "plan",
			Username: "plan_user", Mode: providers.DatabaseModeManaged,
		},
		Secret: []byte("test-password"),
	}
	env := projectDatabaseEnvironment(runtime)
	if env["DB_HOST"] != "devbox-mysql" || env["DB_PORT"] != "3306" ||
		env["DATABASE_HOST"] != "devbox-mysql" || env["DATABASE_PORT"] != "3306" {
		t.Fatalf("unexpected database environment: %#v", env)
	}
	if env["DB_PASSWORD"] != "test-password" || env["DATABASE_PASSWORD"] != "test-password" {
		t.Fatal("database secret was not injected into reserved runtime variables")
	}
}

func TestPHPMySQLDriverDetection(t *testing.T) {
	if hasPHPMySQLDriver(nil) {
		t.Fatal("empty PHP module set must not satisfy MySQL driver requirement")
	}
	if hasPHPMySQLDriver([]RuntimeModule{{Name: "curl"}}) {
		t.Fatal("unrelated PHP module must not satisfy MySQL driver requirement")
	}
	if !hasPHPMySQLDriver([]RuntimeModule{{Name: "pdo_mysql"}}) {
		t.Fatal("pdo_mysql must satisfy MySQL driver requirement")
	}
	if !hasPHPMySQLDriver([]RuntimeModule{{Name: "mysqli"}}) {
		t.Fatal("mysqli must satisfy MySQL driver requirement")
	}
}

func TestManagedEnvironmentPrecedenceDatabaseThenProjectThenRuntimeDefaults(t *testing.T) {
	spec := containerspec.DeploymentSpec{
		Environment: map[string]string{
			"APP_ENV": "runtime-default",
			"DB_HOST": "runtime-db-host",
		},
	}
	mergeProjectEnvironment(&spec, runtimes.ResolvedEnvironment{
		Plain: map[string]string{
			"APP_ENV": "project-value",
			"DB_HOST": "project-db-host",
		},
		Sensitive: map[string]string{
			"API_TOKEN": "project-secret",
		},
	})
	mergeDatabaseEnvironment(&spec, map[string]string{
		"DB_HOST":     "devbox-mysql",
		"DB_PASSWORD": "database-secret",
	})

	if spec.Environment["APP_ENV"] != "project-value" {
		t.Fatalf("project environment must override runtime default, got %q", spec.Environment["APP_ENV"])
	}
	if _, exists := spec.Environment["DB_HOST"]; exists {
		t.Fatal("reserved DB_HOST must be removed from plain runtime environment")
	}
	if spec.SensitiveEnvironment["DB_HOST"] != "devbox-mysql" {
		t.Fatalf("database binding must override project DB_HOST, got %q", spec.SensitiveEnvironment["DB_HOST"])
	}
	if spec.SensitiveEnvironment["DB_PASSWORD"] != "database-secret" {
		t.Fatal("database password was not injected as sensitive runtime environment")
	}
	if spec.SensitiveEnvironment["API_TOKEN"] != "project-secret" {
		t.Fatal("project SecretStore environment was not preserved")
	}
}

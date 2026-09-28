package projects

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/runtimes"
)

func TestDeploymentStateMachine(t *testing.T) {
	states := []string{DeploymentQueued, DeploymentPreparing, DeploymentUpdatingSource, DeploymentDependencies, DeploymentBuilding, DeploymentStarting, DeploymentHealthcheck, DeploymentSuccess}
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
	project := Project{ID: NewID(), Name: "failure-test", Slug: "failure-test", Status: "ready", SourceType: SourceLocal, LocalPath: t.TempDir(), Runtime: "static", ContainerPolicy: ContainerPolicyAuto, DeploymentMode: "docker", CreatedAt: now, UpdatedAt: now}
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
	if len(items) != 1 || items[0].Status != DeploymentFailed || items[0].Stage != DeploymentFailed || !strings.Contains(items[0].Error, "provider unavailable") {
		t.Fatalf("failure state not persisted: %+v", items)
	}
}

type testJobLogger struct{}

func (l *testJobLogger) Log(context.Context, string, string, string, map[string]any) error {
	return nil
}

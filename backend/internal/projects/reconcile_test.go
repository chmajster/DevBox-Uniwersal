package projects

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	jobpkg "github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

func TestReconcileAutoStartQueuesOnceAndMarksReconcilePayload(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "devbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(context.Background(), db, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}

	projectRepo := NewRepository(db)
	now := time.Now().UTC()
	auto := Project{
		ID:              NewID(),
		Name:            "auto",
		Slug:            "auto",
		Status:          "running",
		SourceType:      SourceLocal,
		LocalPath:       t.TempDir(),
		Runtime:         "static",
		ContainerPolicy: ContainerPolicyAuto,
		DeploymentMode:  "docker",
		AutoStart:       true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	manual := auto
	manual.ID = NewID()
	manual.Name = "manual"
	manual.Slug = "manual"
	manual.LocalPath = t.TempDir()
	manual.AutoStart = false
	if err := projectRepo.Create(context.Background(), auto, ""); err != nil {
		t.Fatal(err)
	}
	if err := projectRepo.Create(context.Background(), manual, ""); err != nil {
		t.Fatal(err)
	}

	runner := jobpkg.NewRunner(repository.NewSQLiteJobs(db))
	if err := runner.Register(reconcileNoopHandler{}); err != nil {
		t.Fatal(err)
	}
	service := NewService(projectRepo, NewGitClient(nil), runner, nil, t.TempDir())

	enqueued, err := service.ReconcileAutoStart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(enqueued))
	}
	if got, ok := enqueued[0].Payload["reconcile"].(bool); !ok || !got {
		t.Fatalf("reconcile payload = %#v, want true", enqueued[0].Payload["reconcile"])
	}
	if enqueued[0].ProjectID == nil || *enqueued[0].ProjectID != auto.ID {
		t.Fatalf("unexpected project ID: %#v", enqueued[0].ProjectID)
	}

	second, err := service.ReconcileAutoStart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("second reconciliation enqueued %d duplicate jobs", len(second))
	}
}

type reconcileNoopHandler struct{}

func (reconcileNoopHandler) Type() string { return JobDeploy }

func (reconcileNoopHandler) Run(context.Context, domain.Job) (map[string]any, error) {
	return map[string]any{}, nil
}

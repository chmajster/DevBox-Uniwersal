package runtimes

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	controldb "github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

type testJobRunner struct{}

func (testJobRunner) Register(jobs.Handler) error { return nil }
func (testJobRunner) Enqueue(_ context.Context, request jobs.Request) (domain.Job, error) {
	return domain.Job{ID: "job-test", Type: request.Type, Status: "queued", Payload: request.Payload}, nil
}
func (testJobRunner) Cancel(context.Context, string) error { return nil }
func (testJobRunner) Retry(context.Context, string) (domain.Job, error) {
	return domain.Job{}, nil
}

type testVersionProvider struct{ runtimeType string }

func (p testVersionProvider) RuntimeType() string { return p.runtimeType }
func (p testVersionProvider) ListAvailableVersions(context.Context) ([]AvailableVersion, error) {
	return nil, nil
}
func (p testVersionProvider) DetectInstalled(context.Context) ([]DetectedRuntime, error) { return nil, nil }
func (p testVersionProvider) Install(context.Context, string, string, InstallReporter) (ManagedRuntime, error) {
	return ManagedRuntime{}, nil
}
func (p testVersionProvider) ValidateInstallation(context.Context, Installation) error { return nil }
func (p testVersionProvider) Remove(context.Context, Installation, InstallReporter) error { return nil }

func runtimeVersionTestDB(t *testing.T) *VersionRepository {
	t.Helper()
	db, err := controldb.Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if err := controldb.Migrate(context.Background(), db, migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec("INSERT INTO projects(id,name,slug) VALUES('project-a','Project A','project-a')"); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return NewVersionRepository(db)
}

func TestRuntimeVersionAssignmentAndExecutableResolution(t *testing.T) {
	repo := runtimeVersionTestDB(t)
	ctx := context.Background()

	installation, err := repo.PrepareManagedInstall(ctx, "php", "8.4", "8.4.6", "test")
	if err != nil {
		t.Fatalf("PrepareManagedInstall() error = %v", err)
	}
	if err := repo.MarkInstalled(ctx, installation.ID, "/opt/devbox/php/8.4.6/bin/php", "/opt/devbox/php/8.4.6", map[string]string{
		"php":      "/opt/devbox/php/8.4.6/bin/php",
		"php-fpm":  "/opt/devbox/php/8.4.6/sbin/php-fpm",
	}); err != nil {
		t.Fatalf("MarkInstalled() error = %v", err)
	}

	assignment, err := repo.SetAssignment(ctx, "project-a", "php", installation.ID, "8.4")
	if err != nil {
		t.Fatalf("SetAssignment() error = %v", err)
	}
	if assignment.ResolvedVersion != "8.4.6" {
		t.Fatalf("resolved version = %q, want 8.4.6", assignment.ResolvedVersion)
	}

	execution, err := repo.Execution(ctx, "project-a", "php")
	if err != nil {
		t.Fatalf("Execution() error = %v", err)
	}
	if got := execution.Tools["php"]; got != "/opt/devbox/php/8.4.6/bin/php" {
		t.Fatalf("php executable = %q", got)
	}
	if got := execution.Tools["php-fpm"]; got != "/opt/devbox/php/8.4.6/sbin/php-fpm" {
		t.Fatalf("php-fpm executable = %q", got)
	}

	if err := repo.SetDefault(ctx, "php", installation.ID, "8.4", nil); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}
	defaults, err := repo.Defaults(ctx)
	if err != nil || len(defaults) != 1 || defaults[0].ResolvedVersion != "8.4.6" {
		t.Fatalf("Defaults() = %#v, %v", defaults, err)
	}
}

func TestRuntimeRemovalBlockedWhenProjectUsesInstallation(t *testing.T) {
	repo := runtimeVersionTestDB(t)
	ctx := context.Background()
	installation, err := repo.PrepareManagedInstall(ctx, "node", "22", "22.15.0", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkInstalled(ctx, installation.ID, "/opt/devbox/node/22.15.0/bin/node", "/opt/devbox/node/22.15.0", map[string]string{"node": "/opt/devbox/node/22.15.0/bin/node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetAssignment(ctx, "project-a", "node", installation.ID, "22"); err != nil {
		t.Fatal(err)
	}

	providers := map[string]RuntimeVersionProvider{}
	for _, runtimeType := range []string{"php", "node", "python", "go"} {
		providers[runtimeType] = testVersionProvider{runtimeType: runtimeType}
	}
	service, err := NewVersionService(repo, providers, testJobRunner{}, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.EnqueueRemove(ctx, installation.ID, nil)
	if !errors.Is(err, ErrRuntimeInUse) {
		t.Fatalf("EnqueueRemove() error = %v, want ErrRuntimeInUse", err)
	}
}

func TestRuntimeDuplicateManagedInstallationRejected(t *testing.T) {
	repo := runtimeVersionTestDB(t)
	ctx := context.Background()
	first, err := repo.PrepareManagedInstall(ctx, "go", "1.24", "1.24.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkInstalled(ctx, first.ID, "/opt/devbox/go/1.24.1/bin/go", "/opt/devbox/go/1.24.1", map[string]string{"go": "/opt/devbox/go/1.24.1/bin/go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PrepareManagedInstall(ctx, "go", "1.24.1", "1.24.1", "test"); !errors.Is(err, ErrRuntimeConflict) {
		t.Fatalf("duplicate install error = %v, want ErrRuntimeConflict", err)
	}
}

func TestRuntimeConcurrentInstallReservationIsUnique(t *testing.T) {
	repo := runtimeVersionTestDB(t)
	ctx := context.Background()
	const workers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.PrepareManagedInstall(ctx, "python", "3.13", "3.13.2", "test")
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successful reservations = %d, want exactly 1", successes)
	}
	items, err := repo.ListInstallations(ctx, "python")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("runtime rows = %d, want 1", len(items))
	}
}

func TestRuntimeRejectsInvalidType(t *testing.T) {
	repo := runtimeVersionTestDB(t)
	if _, err := repo.PrepareManagedInstall(context.Background(), "shell", "1", "1.0.0", "test"); !errors.Is(err, ErrInvalidRuntime) {
		t.Fatalf("error = %v, want ErrInvalidRuntime", err)
	}
}

func TestVersionRequirementMatching(t *testing.T) {
	for _, tc := range []struct {
		version     string
		requirement string
		want        bool
	}{
		{"22.15.0", ">=22", true},
		{"20.19.0", ">=22", false},
		{"8.4.6", "^8.3", true},
		{"9.0.0", "^8.3", false},
		{"3.13.2", "~3.13", true},
		{"3.12.9", "~3.13", false},
	} {
		if got := versionSatisfiesRequirement(tc.version, tc.requirement); got != tc.want {
			t.Fatalf("versionSatisfiesRequirement(%q,%q) = %v, want %v", tc.version, tc.requirement, got, tc.want)
		}
	}
}

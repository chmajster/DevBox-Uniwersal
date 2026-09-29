package jobs

import (
	"context"
	"errors"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testHandler struct {
	name string
	run  func(context.Context, domain.Job) (map[string]any, error)
}

func (h testHandler) Type() string { return h.name }
func (h testHandler) Run(ctx context.Context, j domain.Job) (map[string]any, error) {
	return h.run(ctx, j)
}

type recoverableHandler struct {
	testHandler
	retry bool
}

func (h recoverableHandler) RecoverInterrupted(context.Context, domain.Job) (bool, error) {
	return h.retry, nil
}
func testRunner(t *testing.T, workers int) (*Runner, *repository.SQLiteJobs, context.Context) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "jobs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(context.Background(), db, "../../../migrations"); err != nil {
		t.Fatal(err)
	}
	store := repository.NewSQLiteJobs(db)
	runner := NewRunner(store, WithWorkers(workers))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			runner.mu.RLock()
			n := len(runner.active)
			runner.mu.RUnlock()
			if n == 0 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		db.Close()
	})
	return runner, store, ctx
}
func awaitState(t *testing.T, store *repository.SQLiteJobs, id, state string) domain.Job {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	var job domain.Job
	for time.Now().Before(deadline) {
		var err error
		job, err = store.ByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == state {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s has %s, expected %s: %v", id, job.Status, state, job.Error)
	return job
}
func TestWorkersParallelizeDifferentResourcesButSerializeSameKey(t *testing.T) {
	runner, store, ctx := testRunner(t, 3)
	entered := make(chan string, 8)
	release := make(chan struct{})
	var active, max atomic.Int32
	h := testHandler{"test.work", func(ctx context.Context, j domain.Job) (map[string]any, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := max.Load(); n > old; old = max.Load() {
			if max.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- j.ResourceKey
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return map[string]any{"ok": true}, nil
	}}
	if err := runner.Register(h); err != nil {
		t.Fatal(err)
	}
	first, err := runner.Enqueue(ctx, Request{Type: h.name, ResourceKey: "a"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Enqueue(ctx, Request{Type: h.name, ResourceKey: "a"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := runner.Enqueue(ctx, Request{Type: h.name, ResourceKey: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case k := <-entered:
			seen[k] = true
		case <-time.After(3 * time.Second):
			t.Fatal("independent resources not started")
		}
	}
	if !seen["a"] || !seen["b"] {
		t.Fatal("same resource ran concurrently")
	}
	select {
	case <-entered:
		t.Fatal("second job for locked resource ran")
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	awaitState(t, store, first.ID, "succeeded")
	awaitState(t, store, second.ID, "succeeded")
	awaitState(t, store, third.ID, "succeeded")
	if max.Load() != 2 {
		t.Fatalf("unexpected concurrency %d", max.Load())
	}
}
func TestCancellationRetainsResourceUntilHandlerStops(t *testing.T) {
	runner, store, ctx := testRunner(t, 2)
	entered := make(chan string, 2)
	unwind := make(chan struct{})
	var count atomic.Int32
	h := testHandler{"test.cancel", func(c context.Context, j domain.Job) (map[string]any, error) {
		entered <- j.ID
		if count.Add(1) == 1 {
			<-c.Done()
			select {
			case <-unwind:
			case <-ctx.Done():
			}
			return nil, c.Err()
		}
		return nil, nil
	}}
	if err := runner.Register(h); err != nil {
		t.Fatal(err)
	}
	a, err := runner.Enqueue(ctx, Request{Type: h.name, ResourceKey: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := runner.Enqueue(ctx, Request{Type: h.name, ResourceKey: "locked"})
	if err != nil {
		t.Fatal(err)
	}
	if err = runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	if err = runner.Cancel(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, store, a.ID, "cancelled")
	select {
	case <-entered:
		t.Fatal("lock released before cleanup completed")
	case <-time.After(80 * time.Millisecond):
	}
	close(unwind)
	awaitState(t, store, b.ID, "succeeded")
}
func TestPanicDoesNotLeakPayloadAndRunnerContinues(t *testing.T) {
	runner, store, ctx := testRunner(t, 1)
	if err := runner.Register(testHandler{"test.panic", func(context.Context, domain.Job) (map[string]any, error) { panic("private-password") }}); err != nil {
		t.Fatal(err)
	}
	if err := runner.Register(testHandler{"test.ok", func(context.Context, domain.Job) (map[string]any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	a, _ := runner.Enqueue(ctx, Request{Type: "test.panic"})
	b, _ := runner.Enqueue(ctx, Request{Type: "test.ok"})
	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	failed := awaitState(t, store, a.ID, "failed")
	if failed.Error == nil || strings.Contains(*failed.Error, "private-password") {
		t.Fatal("panic payload leaked")
	}
	awaitState(t, store, b.ID, "succeeded")
}
func TestRestartOnlyRetriesExplicitlyReconciledJobs(t *testing.T) {
	runner, store, ctx := testRunner(t, 1)
	noop := func(context.Context, domain.Job) (map[string]any, error) { return nil, nil }
	runner.Register(testHandler{"unsafe", noop})
	runner.Register(recoverableHandler{testHandler{"safe", noop}, true})
	for _, typ := range []string{"safe", "unsafe"} {
		if err := store.CreateJob(ctx, domain.Job{ID: typ, Type: typ, Status: "running", ResourceKey: typ, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.recover(ctx); err != nil {
		t.Fatal(err)
	}
	awaitState(t, store, "safe", "queued")
	awaitState(t, store, "unsafe", "failed")
	runner.admission = func() error { return errors.New("maintenance") }
	if _, err := runner.Enqueue(ctx, Request{Type: "safe"}); err == nil {
		t.Fatal("accepted job during update maintenance")
	}
}
func TestGlobalMaintenanceBlocksOtherResourceClaims(t *testing.T) {
	_, store, ctx := testRunner(t, 3)
	for i, key := range []string{"global:maintenance", "project:a"} {
		if err := store.CreateJob(ctx, domain.Job{ID: key, Type: "test", ResourceKey: key, Status: "queued", CreatedAt: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ClaimNextAvailable(ctx, time.Now(), nil)
	if err != nil || first.ResourceKey != "global:maintenance" {
		t.Fatalf("wrong maintenance claim: %#v %v", first, err)
	}
	if _, err = store.ClaimNextAvailable(ctx, time.Now(), nil); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("job overlapped global maintenance")
	}
}

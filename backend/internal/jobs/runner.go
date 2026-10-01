package jobs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

type Runner struct {
	store    *repository.SQLiteJobs
	mu       sync.RWMutex
	handlers map[string]Handler
	active   map[string]context.CancelFunc
	started  bool
}

func NewRunner(store *repository.SQLiteJobs) *Runner {
	return &Runner{store: store, handlers: map[string]Handler{}, active: map[string]context.CancelFunc{}}
}

func (r *Runner) Register(handler Handler) error {
	if handler == nil || handler.Type() == "" {
		return errors.New("job handler type is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[handler.Type()]; exists {
		return fmt.Errorf("job handler %q already registered", handler.Type())
	}
	r.handlers[handler.Type()] = handler
	return nil
}

func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("job runner already started")
	}
	r.started = true
	r.mu.Unlock()
	if err := r.store.RequeueRunning(ctx); err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	}
	go r.loop(ctx)
	return nil
}

func (r *Runner) Enqueue(ctx context.Context, request Request) (domain.Job, error) {
	r.mu.RLock()
	_, registered := r.handlers[request.Type]
	r.mu.RUnlock()
	if !registered {
		return domain.Job{}, fmt.Errorf("provider unavailable: job handler %q", request.Type)
	}
	job := domain.Job{ID: jobID(), Type: request.Type, Status: "queued", ProjectID: request.ProjectID, ApplicationID: request.ApplicationID, RequestedBy: request.RequestedBy, Payload: request.Payload, CreatedAt: time.Now().UTC()}
	if err := r.store.CreateJob(ctx, job); err != nil {
		return domain.Job{}, err
	}
	_ = r.store.AppendLog(ctx, job.ID, "info", "job.queued", nil)
	return job, nil
}

func (r *Runner) Cancel(ctx context.Context, jobID string) error {
	if err := r.store.CancelJob(ctx, jobID, time.Now().UTC()); err != nil {
		return err
	}
	r.mu.RLock()
	cancel := r.active[jobID]
	r.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	_ = r.store.AppendLog(ctx, jobID, "warn", "job.cancelled", nil)
	return nil
}

func (r *Runner) Retry(ctx context.Context, jobID string) (domain.Job, error) {
	old, err := r.store.ByID(ctx, jobID)
	if err != nil {
		return domain.Job{}, err
	}
	if old.Status != "failed" && old.Status != "cancelled" {
		return domain.Job{}, errors.New("only failed or cancelled jobs can be retried")
	}
	return r.Enqueue(ctx, Request{Type: old.Type, ProjectID: old.ProjectID, ApplicationID: old.ApplicationID, RequestedBy: old.RequestedBy, Payload: old.Payload})
}

func (r *Runner) Log(ctx context.Context, jobID, level, message string, fields map[string]any) error {
	return r.store.AppendLog(ctx, jobID, level, message, fields)
}

func (r *Runner) loop(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runOne(ctx)
		}
	}
}

func (r *Runner) runOne(parent context.Context) {
	job, err := r.store.ClaimNext(parent, time.Now().UTC())
	if err != nil {
		return
	}
	r.mu.RLock()
	handler := r.handlers[job.Type]
	r.mu.RUnlock()
	if handler == nil {
		_ = r.store.FailJob(parent, job.ID, "provider unavailable: no job handler", time.Now().UTC())
		return
	}
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.active[job.ID] = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.active, job.ID)
		r.mu.Unlock()
	}()
	_ = r.store.AppendLog(ctx, job.ID, "info", "job.running", nil)
	result, runErr := handler.Run(ctx, job)
	current, readErr := r.store.ByID(context.Background(), job.ID)
	if readErr == nil && current.Status == "cancelled" {
		_ = r.store.FinishCancellation(context.Background(), job.ID)
		return
	}
	if runErr != nil {
		current, _ := r.store.ByID(context.Background(), job.ID)
		if current.Status == "cancelled" {
			return
		}
		message := runErr.Error()
		_ = r.store.AppendLog(context.Background(), job.ID, "error", "job.failed", map[string]any{"error": message})
		_ = r.store.FailJob(context.Background(), job.ID, message, time.Now().UTC())
		return
	}
	_ = r.store.AppendLog(context.Background(), job.ID, "info", "job.succeeded", nil)
	_ = r.store.CompleteJob(context.Background(), job.ID, result, time.Now().UTC())
}

func jobID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

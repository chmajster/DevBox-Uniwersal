package jobs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/repository"
)

type activeJob struct {
	cancel context.CancelFunc
	key    string
}
type Runner struct {
	store     *repository.SQLiteJobs
	mu        sync.RWMutex
	handlers  map[string]Handler
	active    map[string]activeJob
	started   bool
	workers   int
	wake      chan struct{}
	admission func() error
}
type Option func(*Runner)

func WithAdmissionCheck(check func() error) Option { return func(r *Runner) { r.admission = check } }

func WithWorkers(n int) Option {
	return func(r *Runner) {
		if n >= 1 && n <= 16 {
			r.workers = n
		}
	}
}
func NewRunner(store *repository.SQLiteJobs, options ...Option) *Runner {
	r := &Runner{store: store, handlers: map[string]Handler{}, active: map[string]activeJob{}, workers: 4, wake: make(chan struct{}, 1)}
	for _, option := range options {
		option(r)
	}
	return r
}
func (r *Runner) Register(h Handler) error {
	if h == nil || h.Type() == "" {
		return errors.New("job handler type is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return errors.New("register job handlers before starting the runner")
	}
	if _, ok := r.handlers[h.Type()]; ok {
		return fmt.Errorf("job handler %q already registered", h.Type())
	}
	r.handlers[h.Type()] = h
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
	if err := r.recover(ctx); err != nil {
		r.mu.Lock()
		r.started = false
		r.mu.Unlock()
		return fmt.Errorf("recover jobs: %w", err)
	}
	go r.loop(ctx)
	r.signal()
	return nil
}
func (r *Runner) recover(ctx context.Context) error {
	interrupted, err := r.store.Interrupted(ctx)
	if err != nil {
		return err
	}
	for _, job := range interrupted {
		retry := false
		r.mu.RLock()
		h := r.handlers[job.Type]
		r.mu.RUnlock()
		if recoverable, ok := h.(RestartRecoverable); ok {
			recovery, cancel := context.WithTimeout(ctx, 30*time.Second)
			retry, err = invokeRecovery(recovery, recoverable, job)
			cancel()
			if err != nil {
				retry = false
			}
		}
		if retry {
			if err := r.store.RequeueInterrupted(ctx, job.ID); err != nil {
				return err
			}
			_ = r.store.AppendLog(ctx, job.ID, "warn", "job.recovered.idempotent", nil)
		} else {
			message := "interrupted by service restart; inspect the resource before explicitly retrying"
			if err := r.store.FailJob(ctx, job.ID, message, time.Now().UTC()); err != nil {
				return err
			}
			_ = r.store.AppendLog(ctx, job.ID, "warn", "job.interrupted.review_required", nil)
		}
	}
	return nil
}
func invokeRecovery(ctx context.Context, h RestartRecoverable, j domain.Job) (retry bool, err error) {
	defer func() {
		if recover() != nil {
			retry = false
			err = errors.New("job recovery panicked")
		}
	}()
	return h.RecoverInterrupted(ctx, j)
}
func resourceKey(q Request) string {
	// Host-wide/database lifecycle and control-plane backup jobs are exclusive.
	if strings.HasPrefix(q.Type, "system.backup.") || q.Type == "database.mysql.action" {
		return "global:maintenance"
	}
	if strings.HasPrefix(q.Type, "plugin.") {
		return "host:packages"
	}
	if q.ProjectID != nil && *q.ProjectID != "" {
		return "project:" + *q.ProjectID
	}
	if q.ResourceKey != "" {
		return q.ResourceKey
	}
	for _, key := range []string{"project_id", "database_id", "script_app_id"} {
		if id, ok := q.Payload[key].(string); ok && id != "" {
			return key + ":" + id
		}
	}
	return "global:" + strings.Split(q.Type, ".")[0]
}
func (r *Runner) Enqueue(ctx context.Context, q Request) (domain.Job, error) {
	if r.admission != nil {
		if err := r.admission(); err != nil {
			return domain.Job{}, err
		}
	}
	r.mu.RLock()
	_, ok := r.handlers[q.Type]
	r.mu.RUnlock()
	if !ok {
		return domain.Job{}, fmt.Errorf("provider unavailable: job handler %q", q.Type)
	}
	j := domain.Job{ID: jobID(), Type: q.Type, Status: "queued", ResourceKey: resourceKey(q), ProjectID: q.ProjectID, RequestedBy: q.RequestedBy, Payload: q.Payload, CreatedAt: time.Now().UTC()}
	if err := r.store.CreateJob(ctx, j); err != nil {
		return domain.Job{}, err
	}
	_ = r.store.AppendLog(ctx, j.ID, "info", "job.queued", nil)
	r.signal()
	return j, nil
}
func (r *Runner) Cancel(ctx context.Context, id string) error {
	if err := r.store.CancelJob(ctx, id, time.Now().UTC()); err != nil {
		return err
	}
	r.mu.RLock()
	active := r.active[id]
	r.mu.RUnlock()
	if active.cancel != nil {
		active.cancel()
	}
	_ = r.store.AppendLog(ctx, id, "warn", "job.cancelled", nil)
	r.signal()
	return nil
}
func (r *Runner) Retry(ctx context.Context, id string) (domain.Job, error) {
	old, err := r.store.ByID(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if old.Status != "failed" && old.Status != "cancelled" {
		return domain.Job{}, errors.New("only failed or cancelled jobs can be retried")
	}
	return r.Enqueue(ctx, Request{Type: old.Type, ResourceKey: old.ResourceKey, ProjectID: old.ProjectID, RequestedBy: old.RequestedBy, Payload: old.Payload})
}
func (r *Runner) Log(ctx context.Context, id, level, message string, fields map[string]any) error {
	return r.store.AppendLog(ctx, id, level, message, fields)
}
func (r *Runner) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *Runner) loop(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
		for ctx.Err() == nil {
			r.mu.Lock()
			if len(r.active) >= r.workers {
				r.mu.Unlock()
				break
			}
			keys := make([]string, 0, len(r.active))
			for _, a := range r.active {
				keys = append(keys, a.key)
			}
			job, err := r.store.ClaimNextAvailable(ctx, time.Now().UTC(), keys)
			if err != nil {
				r.mu.Unlock()
				break
			}
			handler := r.handlers[job.Type]
			worker, cancel := context.WithCancel(ctx)
			r.active[job.ID] = activeJob{cancel: cancel, key: job.ResourceKey}
			r.mu.Unlock()
			go r.execute(worker, job, handler, cancel)
		}
	}
}
func (r *Runner) execute(ctx context.Context, j domain.Job, h Handler, cancel context.CancelFunc) {
	defer func() { cancel(); r.mu.Lock(); delete(r.active, j.ID); r.mu.Unlock(); r.signal() }()
	_ = r.store.AppendLog(ctx, j.ID, "info", "job.running", nil)
	var result map[string]any
	var err error
	if h == nil {
		err = errors.New("provider unavailable: no job handler")
	} else {
		result, err = invoke(ctx, h, j)
	}
	final, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	current, readErr := r.store.ByID(final, j.ID)
	if readErr != nil || current.Status == "cancelled" {
		return
	}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = r.store.AppendLog(final, j.ID, "error", "job.failed", map[string]any{"error": err.Error()})
		_ = r.store.FailJob(final, j.ID, err.Error(), time.Now().UTC())
		return
	}
	if err = r.store.CompleteJob(final, j.ID, result, time.Now().UTC()); err != nil {
		_ = r.store.FailJob(final, j.ID, "could not persist job result", time.Now().UTC())
		return
	}
	_ = r.store.AppendLog(final, j.ID, "info", "job.succeeded", nil)
}
func invoke(ctx context.Context, h Handler, j domain.Job) (result map[string]any, err error) {
	// Panic payloads may contain secrets: do not interpolate or persist them.
	defer func() {
		if recover() != nil {
			result = nil
			err = errors.New("job handler panicked; operation stopped, inspect its resource before retrying")
		}
	}()
	return h.Run(ctx, j)
}
func jobID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable")
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

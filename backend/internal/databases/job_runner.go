package databases

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
	"github.com/chmajster/DevBox-Uniwersal/backend/internal/jobs"
)

var _ jobs.JobRunner = (*SQLiteJobRunner)(nil)

type SQLiteJobRunner struct {
	db       *sql.DB
	mu       sync.Mutex
	handlers map[string]jobs.Handler
	cancel   map[string]context.CancelFunc
}

func NewSQLiteJobRunner(db *sql.DB) *SQLiteJobRunner {
	return &SQLiteJobRunner{
		db:       db,
		handlers: make(map[string]jobs.Handler),
		cancel:   make(map[string]context.CancelFunc),
	}
}

func (r *SQLiteJobRunner) Register(handler jobs.Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if handler == nil || handler.Type() == "" {
		return errors.New("job handler type is required")
	}
	if _, exists := r.handlers[handler.Type()]; exists {
		return fmt.Errorf("job handler %q already registered", handler.Type())
	}
	r.handlers[handler.Type()] = handler
	return nil
}

func (r *SQLiteJobRunner) Enqueue(ctx context.Context, request jobs.Request) (domain.Job, error) {
	r.mu.Lock()
	handler := r.handlers[request.Type]
	r.mu.Unlock()
	if handler == nil {
		return domain.Job{}, fmt.Errorf("job handler %q is not registered", request.Type)
	}
	payload, err := json.Marshal(request.Payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("marshal job payload: %w", err)
	}
	now := time.Now().UTC()
	job := domain.Job{
		ID:          newID(),
		Type:        request.Type,
		Status:      "queued",
		ProjectID:   request.ProjectID,
		RequestedBy: request.RequestedBy,
		Payload:     request.Payload,
		CreatedAt:   now,
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO jobs(id,type,status,project_id,requested_by,payload_json,created_at)
		VALUES(?,?,?,?,?,?,?)`,
		job.ID, job.Type, job.Status, job.ProjectID, job.RequestedBy, string(payload), now.Format(time.RFC3339Nano))
	if err != nil {
		return domain.Job{}, fmt.Errorf("enqueue database job: %w", err)
	}
	go r.execute(job, handler)
	return job, nil
}

func (r *SQLiteJobRunner) execute(job domain.Job, handler jobs.Handler) {
	var status string
	if err := r.db.QueryRow(`SELECT status FROM jobs WHERE id=?`, job.ID).Scan(&status); err != nil || status == "cancelled" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancel[job.ID] = cancel
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.cancel, job.ID)
		r.mu.Unlock()
	}()

	started := time.Now().UTC()
	_, _ = r.db.Exec(`UPDATE jobs SET status='running',started_at=? WHERE id=? AND status='queued'`, started.Format(time.RFC3339Nano), job.ID)
	r.log(job.ID, "info", "job started")

	result, err := handler.Run(ctx, job)
	finished := time.Now().UTC()
	if err != nil {
		message := sanitizeJobError(err)
		_, _ = r.db.Exec(`UPDATE jobs SET status='failed',error=?,finished_at=? WHERE id=? AND status!='cancelled'`, message, finished.Format(time.RFC3339Nano), job.ID)
		r.log(job.ID, "error", "job failed: "+message)
		return
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		message := "marshal job result failed"
		_, _ = r.db.Exec(`UPDATE jobs SET status='failed',error=?,finished_at=? WHERE id=? AND status!='cancelled'`, message, finished.Format(time.RFC3339Nano), job.ID)
		r.log(job.ID, "error", message)
		return
	}
	_, _ = r.db.Exec(`UPDATE jobs SET status='succeeded',result_json=?,finished_at=? WHERE id=? AND status!='cancelled'`, string(payload), finished.Format(time.RFC3339Nano), job.ID)
	r.log(job.ID, "info", "job completed")
}

func (r *SQLiteJobRunner) Cancel(ctx context.Context, jobID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE jobs SET status='cancelled',finished_at=? WHERE id=? AND status IN ('queued','running')`, time.Now().UTC().Format(time.RFC3339Nano), jobID)
	if err != nil {
		return fmt.Errorf("cancel database job: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	r.mu.Lock()
	cancel := r.cancel[jobID]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.log(jobID, "warn", "job cancelled")
	return nil
}

func (r *SQLiteJobRunner) Retry(ctx context.Context, jobID string) (domain.Job, error) {
	var jobType string
	var projectID, requestedBy, payloadJSON sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT type,project_id,requested_by,payload_json FROM jobs WHERE id=?`, jobID).
		Scan(&jobType, &projectID, &requestedBy, &payloadJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, ErrNotFound
	}
	if err != nil {
		return domain.Job{}, fmt.Errorf("load job for retry: %w", err)
	}
	var payload map[string]any
	if payloadJSON.Valid && payloadJSON.String != "" {
		if err := json.Unmarshal([]byte(payloadJSON.String), &payload); err != nil {
			return domain.Job{}, fmt.Errorf("decode retry payload: %w", err)
		}
	}
	var projectPtr, requestedPtr *string
	if projectID.Valid {
		projectPtr = &projectID.String
	}
	if requestedBy.Valid {
		requestedPtr = &requestedBy.String
	}
	return r.Enqueue(ctx, jobs.Request{Type: jobType, ProjectID: projectPtr, RequestedBy: requestedPtr, Payload: payload})
}

func (r *SQLiteJobRunner) log(jobID, level, message string) {
	_, _ = r.db.Exec(`INSERT INTO job_logs(job_id,level,message,fields_json,created_at) VALUES(?,?,?,'{}',?)`,
		jobID, level, message, time.Now().UTC().Format(time.RFC3339Nano))
}

func sanitizeJobError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

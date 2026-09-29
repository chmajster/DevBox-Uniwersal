package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

func (r *SQLiteJobs) CreateJob(ctx context.Context, job domain.Job) error {
	payload, err := json.Marshal(job.Payload)
	if err != nil {
		return fmt.Errorf("marshal job payload: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO jobs(id,type,status,resource_key,project_id,requested_by,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, job.ID, job.Type, job.Status, job.ResourceKey, job.ProjectID, job.RequestedBy, string(payload), job.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

func (r *SQLiteJobs) ClaimNext(ctx context.Context, started time.Time) (domain.Job, error) {
	return r.ClaimNextAvailable(ctx, started, nil)
}

// Reserved keys include cancelled handlers which have not exited yet. SQL
// running rows also protect callers outside the dispatch loop.
func (r *SQLiteJobs) ClaimNextAvailable(ctx context.Context, started time.Time, reserved []string) (domain.Job, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Job{}, err
	}
	defer tx.Rollback()
	query := `SELECT id FROM jobs j WHERE status='queued'
 AND NOT EXISTS(SELECT 1 FROM jobs a WHERE a.status='running' AND
 (a.resource_key=j.resource_key OR a.resource_key='global:maintenance' OR j.resource_key='global:maintenance'))`
	args := []any{}
	for _, key := range reserved {
		if key == "global:maintenance" {
			return domain.Job{}, ErrNotFound
		}
		query += ` AND j.resource_key<>?`
		args = append(args, key)
	}
	if len(reserved) > 0 {
		query += ` AND j.resource_key<>'global:maintenance'`
	}
	query += ` ORDER BY created_at ASC,id ASC LIMIT 1`
	var id string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, ErrNotFound
		}
		return domain.Job{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET status='running',started_at=? WHERE id=? AND status='queued'`, started.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return domain.Job{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return domain.Job{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return domain.Job{}, err
	}
	return r.ByID(ctx, id)
}

func (r *SQLiteJobs) Interrupted(ctx context.Context) ([]domain.Job, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,type,status,resource_key,project_id,requested_by,payload_json,result_json,error,created_at,started_at,finished_at FROM jobs WHERE status='running' ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Job
	for rows.Next() {
		job, err := scanJob(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}
func (r *SQLiteJobs) RequeueInterrupted(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE jobs SET status='queued',started_at=NULL,finished_at=NULL,error=NULL WHERE id=? AND status='running'`, id)
	return err
}

func (r *SQLiteJobs) CompleteJob(ctx context.Context, id string, result map[string]any, finished time.Time) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE jobs SET status='succeeded',result_json=?,error=NULL,finished_at=? WHERE id=? AND status='running'`, string(encoded), finished.UTC().Format(time.RFC3339Nano), id)
	return err
}
func (r *SQLiteJobs) FailJob(ctx context.Context, id, message string, finished time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE jobs SET status='failed',error=?,finished_at=? WHERE id=? AND status IN ('queued','running')`, message, finished.UTC().Format(time.RFC3339Nano), id)
	return err
}
func (r *SQLiteJobs) CancelJob(ctx context.Context, id string, finished time.Time) error {
	res, err := r.db.ExecContext(ctx, `UPDATE jobs SET status='cancelled',finished_at=? WHERE id=? AND status IN ('queued','running')`, finished.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("job cannot be cancelled")
	}
	return nil
}

func (r *SQLiteJobs) AppendLog(ctx context.Context, jobID, level, message string, fields map[string]any) error {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO job_logs(job_id,level,message,fields_json,created_at) VALUES(?,?,?,?,?)`, jobID, level, message, string(encoded), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

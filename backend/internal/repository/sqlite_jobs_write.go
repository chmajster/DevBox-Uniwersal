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
	_, err = r.db.ExecContext(ctx, `INSERT INTO jobs(id,type,status,project_id,application_id,requested_by,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, job.ID, job.Type, job.Status, job.ProjectID, job.ApplicationID, job.RequestedBy, string(payload), job.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

func (r *SQLiteJobs) ClaimNext(ctx context.Context, started time.Time) (domain.Job, error) {
	for {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return domain.Job{}, err
		}
		var id string
		err = tx.QueryRowContext(ctx, `SELECT id FROM jobs WHERE status='queued' ORDER BY created_at ASC LIMIT 1`).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return domain.Job{}, ErrNotFound
		}
		if err != nil {
			_ = tx.Rollback()
			return domain.Job{}, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE jobs SET status='running',started_at=? WHERE id=? AND status='queued'`, started.UTC().Format(time.RFC3339Nano), id)
		if err != nil {
			_ = tx.Rollback()
			return domain.Job{}, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			_ = tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			return domain.Job{}, err
		}
		return r.ByID(ctx, id)
	}
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
func (r *SQLiteJobs) RequeueRunning(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `UPDATE jobs SET status='queued',started_at=NULL,error=NULL WHERE status='running'`)
	return err
}
func (r *SQLiteJobs) AppendLog(ctx context.Context, jobID, level, message string, fields map[string]any) error {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO job_logs(job_id,level,message,fields_json,created_at) VALUES(?,?,?,?,?)`, jobID, level, message, string(encoded), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/domain"
)

type SQLiteJobs struct{ db *sql.DB }

func NewSQLiteJobs(db *sql.DB) *SQLiteJobs { return &SQLiteJobs{db: db} }

func (r *SQLiteJobs) List(ctx context.Context, limit, offset int) ([]domain.Job, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,type,status,project_id,application_id,requested_by,payload_json,result_json,error,created_at,started_at,finished_at FROM jobs ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	var out []domain.Job
	for rows.Next() {
		job, err := scanJob(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (r *SQLiteJobs) ByID(ctx context.Context, id string) (domain.Job, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id,type,status,project_id,application_id,requested_by,payload_json,result_json,error,created_at,started_at,finished_at FROM jobs WHERE id = ?`, id)
	job, err := scanJob(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, ErrNotFound
	}
	return job, err
}

type scanner func(dest ...any) error

func scanJob(scan scanner) (domain.Job, error) {
	var j domain.Job
	var projectID, applicationID, requestedBy, payloadJSON, resultJSON, errText, created, started, finished sql.NullString
	if err := scan(&j.ID, &j.Type, &j.Status, &projectID, &applicationID, &requestedBy, &payloadJSON, &resultJSON, &errText, &created, &started, &finished); err != nil {
		return domain.Job{}, err
	}
	if projectID.Valid {
		j.ProjectID = &projectID.String
	}
	if applicationID.Valid {
		j.ApplicationID = &applicationID.String
	}
	if requestedBy.Valid {
		j.RequestedBy = &requestedBy.String
	}
	if errText.Valid {
		j.Error = &errText.String
	}
	if payloadJSON.Valid && payloadJSON.String != "" {
		_ = json.Unmarshal([]byte(payloadJSON.String), &j.Payload)
	}
	if resultJSON.Valid && resultJSON.String != "" {
		_ = json.Unmarshal([]byte(resultJSON.String), &j.Result)
	}
	var err error
	j.CreatedAt, err = parseTime(created.String)
	if err != nil {
		return domain.Job{}, err
	}
	if started.Valid {
		t, e := parseTime(started.String)
		if e != nil {
			return domain.Job{}, e
		}
		j.StartedAt = &t
	}
	if finished.Valid {
		t, e := parseTime(finished.String)
		if e != nil {
			return domain.Job{}, e
		}
		j.FinishedAt = &t
	}
	return j, nil
}

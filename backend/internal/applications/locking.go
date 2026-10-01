package applications

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CheckIdle is paired with the service mutation lock and the database unique
// index: a configuration update cannot race a queued/running lifecycle action.
func (r *Repository) CheckIdle(ctx context.Context, id string) error {
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE application_id=? AND (status IN ('queued','running') OR (status='cancelled' AND finished_at IS NULL))`, id).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("%w: an application operation is already queued or running", ErrConflict)
	}
	return nil
}

func (r *Repository) ReleaseApplicationPorts(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE application_port_leases SET state='released',released_at=? WHERE application_id=? AND state='reserved'`, dbTime(time.Now()), id)
	return err
}

// ActiveOperation describes the operation owning the application lock.
type ActiveOperation struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

func (r *Repository) ActiveOperation(ctx context.Context, id string) (*ActiveOperation, error) {
	var op ActiveOperation
	err := r.db.QueryRowContext(ctx, `SELECT id,type,status FROM jobs WHERE application_id=? AND (status IN ('queued','running') OR (status='cancelled' AND finished_at IS NULL)) ORDER BY created_at DESC LIMIT 1`, id).Scan(&op.ID, &op.Type, &op.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

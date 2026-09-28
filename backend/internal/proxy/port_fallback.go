package proxy

import (
	"context"
	"errors"
	"fmt"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

var _ providers.SequentialPortAllocator = (*PortManager)(nil)
var _ providers.PortLeaseOwner = (*PortManager)(nil)

// ReserveFromOwned uses the requested port as a lower bound, then tries +1. The
// unique database reservation, not the preliminary socket probe, arbitrates
// concurrent DevBox deployments. Non-collision errors must not be swallowed.
func (m *PortManager) ReserveFromOwned(ctx context.Context, projectID, purpose string, start int) (providers.PortReservation, error) {
	if start < 1 || start > 65535 {
		return providers.PortReservation{}, fmt.Errorf("%w: starting port must be between 1 and 65535", ErrInvalidInput)
	}
	for port := start; port <= 65535; port++ {
		if err := ctx.Err(); err != nil {
			return providers.PortReservation{}, err
		}
		owned, err := m.Owns(ctx, projectID, purpose, port)
		if err != nil {
			return providers.PortReservation{}, err
		}
		if owned {
			return providers.PortReservation{PortLease: providers.PortLease{Port: port, ProjectID: projectID, Purpose: purpose}, Reused: true}, nil
		}
		record, err := m.ReserveExact(ctx, projectID, purpose, port)
		if err == nil {
			return providers.PortReservation{PortLease: providers.PortLease{Port: record.Port, ProjectID: projectID, Purpose: purpose}}, nil
		}
		if !errors.Is(err, ErrPortInUse) && !errors.Is(err, ErrConflict) {
			return providers.PortReservation{}, err
		}
	}
	return providers.PortReservation{}, ErrNoPorts
}

func (m *PortManager) Owns(ctx context.Context, projectID, purpose string, port int) (bool, error) {
	var count int
	err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ports WHERE project_id=? AND purpose=? AND port=? AND state<>'released' AND released_at IS NULL`, projectID, purpose, port).Scan(&count)
	return count == 1, err
}

func (m *PortManager) ReleaseOwned(ctx context.Context, projectID, purpose string, port int) error {
	_, err := m.db.ExecContext(ctx, `UPDATE ports SET state='released', released_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE project_id=? AND purpose=? AND port=? AND state<>'released'`, projectID, purpose, port)
	return err
}

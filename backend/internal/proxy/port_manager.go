package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

type socketProbe func(int) (bool, error)

type PortManager struct {
	db    *sql.DB
	start int
	end   int
	probe socketProbe
}

var _ providers.PortAllocator = (*PortManager)(nil)

func NewPortManager(db *sql.DB, start, end int) *PortManager {
	return &PortManager{db: db, start: start, end: end, probe: probeSocket}
}

func (m *PortManager) Allocate(ctx context.Context, projectID, purpose string) (PortRecord, error) {
	return m.AllocateFrom(ctx, projectID, purpose, m.start)
}

func (m *PortManager) AllocateFrom(ctx context.Context, projectID, purpose string, start int) (PortRecord, error) {
	if projectID == "" || purpose == "" {
		return PortRecord{}, fmt.Errorf("%w: project and purpose are required", ErrInvalidInput)
	}
	if start < m.start {
		start = m.start
	}
	if start > m.end {
		return PortRecord{}, ErrNoPorts
	}
	for port := start; port <= m.end; port++ {
		record, err := m.ReserveExact(ctx, projectID, purpose, port)
		if err == nil {
			return record, nil
		}
		if errors.Is(err, ErrPortInUse) || errors.Is(err, ErrConflict) {
			continue
		}
		return PortRecord{}, err
	}
	return PortRecord{}, ErrNoPorts
}

func (m *PortManager) Reserve(ctx context.Context, projectID, purpose string, preferred *int) (providers.PortLease, error) {
	var record PortRecord
	var err error
	if preferred == nil {
		record, err = m.Allocate(ctx, projectID, purpose)
	} else {
		record, err = m.ReserveExact(ctx, projectID, purpose, *preferred)
	}
	if err != nil {
		return providers.PortLease{}, err
	}
	return providers.PortLease{Port: record.Port, ProjectID: projectID, Purpose: purpose}, nil
}

func (m *PortManager) ReserveFrom(ctx context.Context, projectID, purpose string, start int) (providers.PortLease, error) {
	reservation, err := m.ReserveFromOwned(ctx, projectID, purpose, start)
	return reservation.PortLease, err
}

func (m *PortManager) ReserveExact(ctx context.Context, projectID, purpose string, port int) (PortRecord, error) {
	if projectID == "" || purpose == "" {
		return PortRecord{}, fmt.Errorf("%w: project and purpose are required", ErrInvalidInput)
	}
	if port < 1 || port > 65535 {
		return PortRecord{}, fmt.Errorf("%w: port must be between 1 and 65535", ErrInvalidInput)
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return PortRecord{}, fmt.Errorf("begin port reservation: %w", err)
	}
	defer tx.Rollback()

	var state string
	err = tx.QueryRowContext(ctx, "SELECT state FROM ports WHERE port = ?", port).Scan(&state)
	switch {
	case err == nil && state != "released":
		return PortRecord{}, ErrPortInUse
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return PortRecord{}, fmt.Errorf("inspect port lease: %w", err)
	}

	socketAvailable, err := m.probe(port)
	if err != nil {
		return PortRecord{}, fmt.Errorf("probe port %d: %w", port, err)
	}
	if !socketAvailable {
		return PortRecord{}, ErrPortInUse
	}

	now := time.Now().UTC()
	if state == "released" {
		_, err = tx.ExecContext(ctx, `
			UPDATE ports
			SET project_id = ?, purpose = ?, state = 'reserved', created_at = ?, released_at = NULL
			WHERE port = ?
		`, projectID, purpose, formatTime(now), port)
	} else {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ports(id, project_id, port, purpose, state, created_at)
			VALUES(?,?,?,?, 'reserved', ?)
		`, newID(), projectID, port, purpose, formatTime(now))
	}
	if err != nil {
		return PortRecord{}, classifyWriteError("reserve port", err)
	}
	if err := tx.Commit(); err != nil {
		if isConstraintError(err) {
			return PortRecord{}, ErrPortInUse
		}
		return PortRecord{}, fmt.Errorf("commit port reservation: %w", err)
	}
	return m.Inspect(ctx, port)
}

func (m *PortManager) Release(ctx context.Context, port int) error {
	res, err := m.db.ExecContext(ctx, `
		UPDATE ports SET state = 'released', released_at = ?
		WHERE port = ? AND state <> 'released'
	`, formatTime(time.Now().UTC()), port)
	if err != nil {
		return fmt.Errorf("release port: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("release port rows: %w", err)
	}
	if n == 0 {
		var exists int
		if err := m.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM ports WHERE port = ?", port).Scan(&exists); err != nil {
			return fmt.Errorf("inspect released port: %w", err)
		}
		if exists == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (m *PortManager) IsAvailable(ctx context.Context, port int) (bool, error) {
	var state string
	err := m.db.QueryRowContext(ctx, "SELECT state FROM ports WHERE port = ?", port).Scan(&state)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("inspect port database state: %w", err)
	}
	if err == nil && state != "released" {
		return false, nil
	}
	return m.probe(port)
}

func (m *PortManager) Inspect(ctx context.Context, port int) (PortRecord, error) {
	row := m.db.QueryRowContext(ctx, `
		SELECT po.id, po.project_id, p.name, po.port, po.purpose, po.state, po.created_at, po.released_at
		FROM ports po
		LEFT JOIN projects p ON p.id = po.project_id
		WHERE po.port = ?
	`, port)
	record, err := scanPort(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return PortRecord{}, ErrNotFound
	}
	if err != nil {
		return PortRecord{}, fmt.Errorf("inspect port: %w", err)
	}
	available, probeErr := m.probe(port)
	if probeErr != nil {
		return PortRecord{}, fmt.Errorf("probe port %d: %w", port, probeErr)
	}
	record.SocketAvailable = available
	return record, nil
}

func (m *PortManager) List(ctx context.Context) ([]PortRecord, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT po.id, po.project_id, p.name, po.port, po.purpose, po.state, po.created_at, po.released_at
		FROM ports po
		LEFT JOIN projects p ON p.id = po.project_id
		ORDER BY po.port
	`)
	if err != nil {
		return nil, fmt.Errorf("list ports: %w", err)
	}
	defer rows.Close()

	out := make([]PortRecord, 0)
	for rows.Next() {
		record, err := scanPort(rows.Scan)
		if err != nil {
			return nil, err
		}
		available, probeErr := m.probe(record.Port)
		if probeErr != nil {
			return nil, fmt.Errorf("probe port %d: %w", record.Port, probeErr)
		}
		record.SocketAvailable = available
		out = append(out, record)
	}
	return out, rows.Err()
}

func scanPort(scan rowScanner) (PortRecord, error) {
	var p PortRecord
	var projectID, application, released sql.NullString
	var created string
	if err := scan(&p.ID, &projectID, &application, &p.Port, &p.Purpose, &p.State, &created, &released); err != nil {
		return PortRecord{}, err
	}
	if projectID.Valid {
		p.ProjectID = &projectID.String
	}
	if application.Valid {
		p.Application = &application.String
	}
	var err error
	p.CreatedAt, err = parseDBTime(created)
	if err != nil {
		return PortRecord{}, err
	}
	if released.Valid {
		t, err := parseDBTime(released.String)
		if err != nil {
			return PortRecord{}, err
		}
		p.ReleasedAt = &t
	}
	return p, nil
}

func probeSocket(port int) (bool, error) {
	ipv4, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		if isAddressInUse(err) {
			return false, nil
		}
		return false, err
	}
	defer ipv4.Close()
	ipv6, err := net.Listen("tcp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		if errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EPROTONOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL) {
			return true, nil
		}
		if isAddressInUse(err) {
			return false, nil
		}
		return false, err
	}
	return true, ipv6.Close()
}

func isAddressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

func isConstraintError(err error) bool {
	return err != nil && (errors.Is(err, ErrConflict) ||
		containsFold(err.Error(), "constraint"))
}

func containsFold(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		match := true
		for j := range needle {
			a, b := value[i+j], needle[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

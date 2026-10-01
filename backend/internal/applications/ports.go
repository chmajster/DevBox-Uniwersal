package applications

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/providers"
)

var ErrNoPorts = errors.New("no application ports available")

type ApplicationPortAllocator struct {
	db    *sql.DB
	start int
	end   int
}

func NewApplicationPortAllocator(db *sql.DB, start, end int) *ApplicationPortAllocator {
	return &ApplicationPortAllocator{db: db, start: start, end: end}
}

func (a *ApplicationPortAllocator) Reserve(ctx context.Context, applicationID, endpointID, purpose string, preferred *int) (providers.PortLease, error) {
	if strings.TrimSpace(applicationID) == "" || strings.TrimSpace(endpointID) == "" || strings.TrimSpace(purpose) == "" {
		return providers.PortLease{}, fmt.Errorf("%w: application, endpoint and purpose are required", ErrInvalidInput)
	}
	if preferred != nil {
		port, err := a.reserveExact(ctx, applicationID, endpointID, purpose, *preferred)
		if err != nil {
			return providers.PortLease{}, err
		}
		return providers.PortLease{Port: port, ProjectID: applicationID, Purpose: purpose}, nil
	}
	for port := a.start; port <= a.end; port++ {
		reserved, err := a.reserveExact(ctx, applicationID, endpointID, purpose, port)
		if err == nil {
			return providers.PortLease{Port: reserved, ProjectID: applicationID, Purpose: purpose}, nil
		}
		if errors.Is(err, ErrConflict) {
			continue
		}
		return providers.PortLease{}, err
	}
	return providers.PortLease{}, ErrNoPorts
}

func (a *ApplicationPortAllocator) reserveExact(ctx context.Context, applicationID, endpointID, purpose string, port int) (int, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%w: port must be within 1..65535", ErrInvalidInput)
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var state string
	var currentApplication, currentEndpoint, currentPurpose sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT state,application_id,endpoint_id,purpose FROM application_port_leases WHERE port=?`, port).Scan(&state, &currentApplication, &currentEndpoint, &currentPurpose)
	switch {
	case err == nil && state != "released":
		if currentApplication.String == applicationID && currentEndpoint.String == endpointID && currentPurpose.String == purpose {
			_ = tx.Rollback()
			return port, nil
		}
		return 0, ErrConflict
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}
	available, err := probeApplicationPort(port)
	if err != nil {
		return 0, err
	}
	if !available {
		return 0, ErrConflict
	}
	now := dbTime(time.Now())
	if state == "released" {
		_, err = tx.ExecContext(ctx, `UPDATE application_port_leases SET application_id=?,endpoint_id=?,purpose=?,state='reserved',created_at=?,released_at=NULL WHERE port=?`, applicationID, endpointID, purpose, now, port)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO application_port_leases(id,application_id,endpoint_id,port,purpose,state,created_at) VALUES(?,?,?,?,?,'reserved',?)`, NewID(), applicationID, endpointID, port, purpose, now)
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return 0, ErrConflict
		}
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return port, nil
}

func (a *ApplicationPortAllocator) Release(ctx context.Context, applicationID, endpointID string, port int) error {
	res, err := a.db.ExecContext(ctx, `UPDATE application_port_leases SET state='released',released_at=? WHERE application_id=? AND endpoint_id=? AND port=? AND state='reserved'`, dbTime(time.Now()), applicationID, endpointID, port)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func probeApplicationPort(port int) (bool, error) {
	ipv4, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
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
		if errors.Is(err, syscall.EADDRINUSE) {
			return false, nil
		}
		return false, err
	}
	return true, ipv6.Close()
}

package proxy

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/database"
)

func TestPortManagerDetectsDatabaseAndSocketCollisions(t *testing.T) {
	db := testNetworkingDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "INSERT INTO projects(id,name,slug) VALUES('p1','Project One','project-one')"); err != nil {
		t.Fatal(err)
	}

	freePort := temporaryFreePort(t)
	manager := NewPortManager(db, freePort, freePort)
	first, err := manager.ReserveExact(ctx, "p1", "app", freePort)
	if err != nil {
		t.Fatalf("ReserveExact() error = %v", err)
	}
	if first.Port != freePort {
		t.Fatalf("ReserveExact() port = %d", first.Port)
	}
	if _, err := manager.ReserveExact(ctx, "p1", "other", freePort); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("second ReserveExact() error = %v, want ErrPortInUse", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	busyPort, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	busyManager := NewPortManager(db, busyPort, busyPort)
	if _, err := busyManager.Allocate(ctx, "p1", "socket-collision"); !errors.Is(err, ErrNoPorts) {
		t.Fatalf("Allocate() error = %v, want ErrNoPorts", err)
	}
}

func TestPortManagerReleaseAllowsReuse(t *testing.T) {
	db := testNetworkingDB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "INSERT INTO projects(id,name,slug) VALUES('p1','Project One','project-one')"); err != nil {
		t.Fatal(err)
	}
	port := temporaryFreePort(t)
	manager := NewPortManager(db, port, port)
	if _, err := manager.ReserveExact(ctx, "p1", "first", port); err != nil {
		t.Fatal(err)
	}
	if err := manager.Release(ctx, port); err != nil {
		t.Fatal(err)
	}
	record, err := manager.ReserveExact(ctx, "p1", "second", port)
	if err != nil {
		t.Fatalf("reuse ReserveExact() error = %v", err)
	}
	if record.State != "reserved" || record.Purpose != "second" {
		t.Fatalf("unexpected reused record: %+v", record)
	}
}

func testNetworkingDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := t.TempDir()
	db, err := database.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	migrations := filepath.Join("..", "..", "..", "migrations")
	if _, err := os.Stat(migrations); err != nil {
		db.Close()
		t.Fatalf("migrations path unavailable: %v", err)
	}
	if err := database.Migrate(context.Background(), db, migrations); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func temporaryFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

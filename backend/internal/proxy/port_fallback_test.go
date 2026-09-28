package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
)

func TestReserveFromSkipsLeasesAndBusySocketsAndSurvivesRestart(t *testing.T) {
	db := testNetworkingDB(t)
	defer db.Close()
	ctx := context.Background()
	for _, id := range []string{"first", "second", "third"} {
		if _, err := db.Exec(`INSERT INTO projects(id,name,slug) VALUES(?,?,?)`, id, id, id); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewPortManager(db, 20000, 20100)
	manager.probe = func(port int) (bool, error) { return port != 8082, nil }
	first, err := manager.ReserveFromOwned(ctx, "first", "application", 8080)
	if err != nil || first.Port != 8080 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := manager.ReserveFromOwned(ctx, "second", "application", 8080)
	if err != nil || second.Port != 8081 {
		t.Fatalf("second = %+v, %v", second, err)
	}
	third, err := manager.ReserveFromOwned(ctx, "third", "application", 8080)
	if err != nil || third.Port != 8083 {
		t.Fatalf("third = %+v, %v", third, err)
	}
	restarted := NewPortManager(db, 20000, 20100)
	owned, err := restarted.Owns(ctx, "second", "application", 8081)
	if err != nil || !owned {
		t.Fatalf("durable owner = %v, %v", owned, err)
	}
	if err := manager.ReleaseOwned(ctx, "first", "application", 8081); err != nil {
		t.Fatal(err)
	}
	owned, err = manager.Owns(ctx, "second", "application", 8081)
	if err != nil || !owned {
		t.Fatalf("another project released the lease: %v, %v", owned, err)
	}
	if err := manager.ReleaseOwned(ctx, "first", "application", 8080); err != nil {
		t.Fatal(err)
	}
	reused, err := manager.ReserveFromOwned(ctx, "third", "application-https", 8080)
	if err != nil || reused.Port != 8080 {
		t.Fatalf("reuse = %+v, %v", reused, err)
	}
}

func TestReserveFromConcurrentProjectsGetDistinctConsecutivePorts(t *testing.T) {
	db := testNetworkingDB(t)
	defer db.Close()
	manager := NewPortManager(db, 30000, 30001)
	manager.probe = func(int) (bool, error) { return true, nil }
	const count = 8
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("project-%d", i)
		if _, err := db.Exec(`INSERT INTO projects(id,name,slug) VALUES(?,?,?)`, id, id, id); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	ports := make(chan int, count)
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lease, err := manager.ReserveFromOwned(context.Background(), fmt.Sprintf("project-%d", i), "application", 8443)
			if err != nil {
				errs <- err
				return
			}
			ports <- lease.Port
		}(i)
	}
	wg.Wait()
	close(errs)
	close(ports)
	for err := range errs {
		t.Error(err)
	}
	seen := map[int]bool{}
	for port := range ports {
		if seen[port] {
			t.Errorf("duplicate port %d", port)
		}
		seen[port] = true
	}
	for port := 8443; port < 8443+count; port++ {
		if !seen[port] {
			t.Errorf("missing consecutive port %d", port)
		}
	}
}

func TestReserveFromRangeCancellationAndRealErrors(t *testing.T) {
	db := testNetworkingDB(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO projects(id,name,slug) VALUES('p','p','p')`); err != nil {
		t.Fatal(err)
	}
	manager := NewPortManager(db, 8000, 9000)
	manager.probe = func(int) (bool, error) { return false, nil }
	if _, err := manager.ReserveFromOwned(context.Background(), "p", "application", 65535); !errors.Is(err, ErrNoPorts) {
		t.Fatalf("exhaustion: %v", err)
	}
	for _, port := range []int{0, -1, 65536} {
		if _, err := manager.ReserveFromOwned(context.Background(), "p", "application", port); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("range %d: %v", port, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.ReserveFromOwned(ctx, "p", "application", 8080); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	manager.probe = func(int) (bool, error) { return false, syscall.EACCES }
	if _, err := manager.ReserveFromOwned(context.Background(), "p", "application", 8080); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("permission error was hidden: %v", err)
	}
}

func TestAddressInUseDoesNotClassifyPermissionFailureAsCollision(t *testing.T) {
	wrap := func(err error) error {
		return &net.OpError{Op: "listen", Net: "tcp", Err: &os.SyscallError{Syscall: "bind", Err: err}}
	}
	if !isAddressInUse(wrap(syscall.EADDRINUSE)) {
		t.Fatal("real collision not recognized")
	}
	if isAddressInUse(wrap(syscall.EACCES)) || isAddressInUse(context.DeadlineExceeded) {
		t.Fatal("non-collision error classified as busy")
	}
}

func TestPortProbeAlsoChecksIPv6(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 unavailable: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	available, err := probeSocket(port)
	if err != nil {
		t.Fatal(err)
	}
	if available {
		t.Fatalf("IPv6 listener on %d was ignored", port)
	}
}

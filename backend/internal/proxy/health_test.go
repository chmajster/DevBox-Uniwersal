package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPHealthCheckTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHealthChecker(nil)
	result := checker.CheckHTTP(context.Background(), server.URL, 10*time.Millisecond)
	if result.Status != "unhealthy" {
		t.Fatalf("status = %q, want unhealthy", result.Status)
	}
	if result.Error == "" {
		t.Fatal("expected timeout error")
	}
}

func TestTCPHealthCheck(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	checker := NewHealthChecker(nil)
	result := checker.CheckTCP(context.Background(), listener.Addr().String(), time.Second)
	if result.Status != "healthy" {
		t.Fatalf("status = %q error=%q", result.Status, result.Error)
	}
}

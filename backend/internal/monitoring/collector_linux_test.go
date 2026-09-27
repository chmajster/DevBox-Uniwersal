//go:build linux

package monitoring

import (
	"strings"
	"testing"
)

func TestParseCPUStat(t *testing.T) {
	counters, err := parseCPUStat(strings.NewReader("cpu  100 20 30 400 10 5 2 1\ncpu0 1 2 3 4\n"))
	if err != nil {
		t.Fatalf("parseCPUStat: %v", err)
	}
	if counters.total != 568 {
		t.Fatalf("total=%d want 568", counters.total)
	}
	if counters.idle != 410 {
		t.Fatalf("idle=%d want 410", counters.idle)
	}
}

func TestParseMemInfo(t *testing.T) {
	total, available, err := parseMemInfo(strings.NewReader("MemTotal: 1000 kB\nMemAvailable: 250 kB\n"))
	if err != nil {
		t.Fatalf("parseMemInfo: %v", err)
	}
	if total != 1000*1024 || available != 250*1024 {
		t.Fatalf("unexpected bytes total=%d available=%d", total, available)
	}
}

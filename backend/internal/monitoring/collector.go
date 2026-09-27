package monitoring

import (
	"context"
	"os"
	"runtime"
	"time"
)

type CPUStats struct {
	Available    bool    `json:"available"`
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
}

type MemoryStats struct {
	Available    bool    `json:"available"`
	TotalBytes   uint64  `json:"total_bytes"`
	UsedBytes    uint64  `json:"used_bytes"`
	FreeBytes    uint64  `json:"free_bytes"`
	UsagePercent float64 `json:"usage_percent"`
}

type DiskStats struct {
	Available    bool    `json:"available"`
	Path         string  `json:"path"`
	TotalBytes   uint64  `json:"total_bytes"`
	UsedBytes    uint64  `json:"used_bytes"`
	FreeBytes    uint64  `json:"free_bytes"`
	UsagePercent float64 `json:"usage_percent"`
}

type ProcessStats struct {
	PID                  int    `json:"pid"`
	Goroutines           int    `json:"goroutines"`
	HeapAllocatedBytes   uint64 `json:"heap_allocated_bytes"`
	RuntimeReservedBytes uint64 `json:"runtime_reserved_bytes"`
	HostProcessCount     int    `json:"host_process_count"`
	UptimeSeconds        uint64 `json:"uptime_seconds"`
}

type Snapshot struct {
	CollectedAt       time.Time    `json:"collected_at"`
	HostUptimeSeconds uint64       `json:"host_uptime_seconds"`
	CPU               CPUStats     `json:"cpu"`
	Memory            MemoryStats  `json:"memory"`
	Disk              DiskStats    `json:"disk"`
	Process           ProcessStats `json:"process"`
	Warnings          []string     `json:"warnings,omitempty"`
}

type Collector struct {
	startedAt time.Time
}

func NewCollector() *Collector {
	return &Collector{startedAt: time.Now()}
}

func (c *Collector) Collect(ctx context.Context) (Snapshot, error) {
	snapshot, err := collectPlatform(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	snapshot.CollectedAt = time.Now().UTC()
	snapshot.Process.PID = os.Getpid()
	snapshot.Process.Goroutines = runtime.NumGoroutine()
	snapshot.Process.HeapAllocatedBytes = memory.HeapAlloc
	snapshot.Process.RuntimeReservedBytes = memory.Sys
	snapshot.Process.UptimeSeconds = uint64(time.Since(c.startedAt).Seconds())
	if snapshot.CPU.Cores == 0 {
		snapshot.CPU.Cores = runtime.NumCPU()
	}
	return snapshot, nil
}

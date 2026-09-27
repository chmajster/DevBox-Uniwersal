//go:build !linux

package monitoring

import (
	"context"
	"runtime"
)

func collectPlatform(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		CPU:      CPUStats{Cores: runtime.NumCPU()},
		Disk:     DiskStats{Path: "/"},
		Warnings: []string{"host CPU, RAM, disk and uptime metrics are not implemented for this operating system; use the platform service provider"},
	}, nil
}

//go:build linux

package monitoring

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type cpuCounters struct {
	total uint64
	idle  uint64
}

func collectPlatform(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	snapshot.Disk.Path = "/"

	first, err := readCPUCounters()
	if err != nil {
		snapshot.Warnings = append(snapshot.Warnings, "cpu metrics unavailable: "+err.Error())
	} else {
		timer := time.NewTimer(120 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Snapshot{}, ctx.Err()
		case <-timer.C:
		}
		second, secondErr := readCPUCounters()
		if secondErr != nil {
			snapshot.Warnings = append(snapshot.Warnings, "cpu metrics unavailable: "+secondErr.Error())
		} else if second.total > first.total {
			totalDelta := second.total - first.total
			idleDelta := second.idle - first.idle
			snapshot.CPU.Available = true
			snapshot.CPU.UsagePercent = percent(totalDelta-idleDelta, totalDelta)
		}
	}

	if file, openErr := os.Open("/proc/meminfo"); openErr != nil {
		snapshot.Warnings = append(snapshot.Warnings, "memory metrics unavailable: "+openErr.Error())
	} else {
		total, available, parseErr := parseMemInfo(file)
		_ = file.Close()
		if parseErr != nil {
			snapshot.Warnings = append(snapshot.Warnings, "memory metrics unavailable: "+parseErr.Error())
		} else {
			snapshot.Memory.Available = true
			snapshot.Memory.TotalBytes = total
			snapshot.Memory.FreeBytes = available
			snapshot.Memory.UsedBytes = total - available
			snapshot.Memory.UsagePercent = percent(snapshot.Memory.UsedBytes, total)
		}
	}

	var stat syscall.Statfs_t
	if statErr := syscall.Statfs("/", &stat); statErr != nil {
		snapshot.Warnings = append(snapshot.Warnings, "disk metrics unavailable: "+statErr.Error())
	} else {
		blockSize := uint64(stat.Bsize)
		total := stat.Blocks * blockSize
		free := stat.Bavail * blockSize
		snapshot.Disk.Available = true
		snapshot.Disk.TotalBytes = total
		snapshot.Disk.FreeBytes = free
		snapshot.Disk.UsedBytes = total - free
		snapshot.Disk.UsagePercent = percent(snapshot.Disk.UsedBytes, total)
	}

	if uptimeData, readErr := os.ReadFile("/proc/uptime"); readErr != nil {
		snapshot.Warnings = append(snapshot.Warnings, "host uptime unavailable: "+readErr.Error())
	} else {
		fields := strings.Fields(string(uptimeData))
		if len(fields) > 0 {
			seconds, parseErr := strconv.ParseFloat(fields[0], 64)
			if parseErr == nil && seconds >= 0 {
				snapshot.HostUptimeSeconds = uint64(seconds)
			}
		}
	}

	if entries, readErr := os.ReadDir("/proc"); readErr == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, parseErr := strconv.Atoi(entry.Name()); parseErr == nil {
				snapshot.Process.HostProcessCount++
			}
		}
	}

	return snapshot, nil
}

func readCPUCounters() (cpuCounters, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return cpuCounters{}, err
	}
	defer file.Close()
	return parseCPUStat(file)
}

func parseCPUStat(reader io.Reader) (cpuCounters, error) {
	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return cpuCounters{}, err
		}
		return cpuCounters{}, fmt.Errorf("missing aggregate cpu line")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, fmt.Errorf("invalid aggregate cpu line")
	}

	var counters []uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuCounters{}, fmt.Errorf("parse cpu counter: %w", err)
		}
		counters = append(counters, value)
	}
	var total uint64
	for _, value := range counters {
		total += value
	}
	idle := counters[3]
	if len(counters) > 4 {
		idle += counters[4]
	}
	return cpuCounters{total: total, idle: idle}, nil
}

func parseMemInfo(reader io.Reader) (uint64, uint64, error) {
	scanner := bufio.NewScanner(reader)
	var totalKB uint64
	var availableKB uint64
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			totalKB = value
		case "MemAvailable":
			availableKB = value
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if totalKB == 0 || availableKB == 0 || availableKB > totalKB {
		return 0, 0, fmt.Errorf("MemTotal/MemAvailable missing or invalid")
	}
	return totalKB * 1024, availableKB * 1024, nil
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}

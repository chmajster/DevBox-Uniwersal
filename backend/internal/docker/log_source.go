package docker

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chmajster/DevBox-Uniwersal/backend/internal/operations"
)

type OperationsLogSource struct {
	provider *CLIProvider
}

func NewOperationsLogSource(provider *CLIProvider) *OperationsLogSource {
	return &OperationsLogSource{provider: provider}
}

func (s *OperationsLogSource) Name() string { return "docker" }

func (s *OperationsLogSource) List(ctx context.Context, filter operations.LogFilter) ([]operations.LogEntry, error) {
	containers, err := s.provider.ListContainers(ctx)
	if err != nil {
		return nil, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	tail := limit
	if tail > 200 {
		tail = 200
	}
	items := make([]operations.LogEntry, 0, limit)
	for _, container := range containers {
		stdout, stderr, err := s.provider.runner.Run(ctx, "container", "logs", "--timestamps", "--tail", fmt.Sprintf("%d", tail), container.ID)
		if err != nil {
			continue
		}
		for _, stream := range []struct {
			data  []byte
			level string
		}{
			{data: stdout, level: "info"},
			{data: stderr, level: "error"},
		} {
			for _, line := range strings.Split(strings.TrimSpace(string(stream.data)), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				created, message := parseDockerLogLine(line)
				entry := operations.LogEntry{
					Cursor:    created.UnixNano(),
					ID:        fmt.Sprintf("docker:%s:%d", container.ID, created.UnixNano()),
					Source:    "docker",
					Level:     stream.level,
					Message:   message,
					Fields:    map[string]any{"container_id": container.ID, "container_name": container.Name},
					CreatedAt: created,
				}
				if dockerLogMatches(entry, filter) {
					items = append(items, entry)
				}
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func parseDockerLogLine(line string) (time.Time, string) {
	fields := strings.Fields(line)
	if len(fields) > 1 {
		if parsed, err := time.Parse(time.RFC3339Nano, fields[0]); err == nil {
			return parsed.UTC(), strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
		}
	}
	return time.Now().UTC(), line
}

func dockerLogMatches(entry operations.LogEntry, filter operations.LogFilter) bool {
	if entry.Cursor <= filter.AfterCursor {
		return false
	}
	if filter.Level != "" && !strings.EqualFold(entry.Level, filter.Level) {
		return false
	}
	if filter.Search != "" && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(filter.Search)) {
		return false
	}
	if filter.Since != nil && entry.CreatedAt.Before(*filter.Since) {
		return false
	}
	if filter.Until != nil && entry.CreatedAt.After(*filter.Until) {
		return false
	}
	return true
}

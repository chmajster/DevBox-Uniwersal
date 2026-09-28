package operations

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type AggregateLogSource struct {
	name     string
	registry *Registry
}

func NewAggregateLogSource(registry *Registry) *AggregateLogSource {
	return &AggregateLogSource{name: "all", registry: registry}
}

func (s *AggregateLogSource) Name() string { return s.name }

func (s *AggregateLogSource) List(ctx context.Context, filter LogFilter) ([]LogEntry, error) {
	sourceFilter := filter
	sourceFilter.AfterCursor = 0
	sourceFilter.Limit = normalizeLimit(filter.Limit)

	items := make([]LogEntry, 0, sourceFilter.Limit*2)
	for _, name := range s.registry.Sources() {
		if name == s.name {
			continue
		}
		source, ok := s.registry.Source(name)
		if !ok {
			continue
		}
		entries, err := source.List(ctx, sourceFilter)
		if err != nil {
			now := time.Now().UTC()
			entries = []LogEntry{{
				Cursor:    now.UnixNano(),
				ID:        fmt.Sprintf("all:%s:error:%d", name, now.UnixNano()),
				Source:    name,
				Level:     "warn",
				Message:   "log source unavailable: " + err.Error(),
				CreatedAt: now,
			}}
		}
		for _, entry := range entries {
			entry.Cursor = entry.CreatedAt.UnixNano()
			if entry.Cursor <= 0 {
				entry.Cursor = time.Now().UTC().UnixNano()
			}
			entry.ID = entry.Source + ":" + entry.ID
			if entry.Cursor <= filter.AfterCursor {
				continue
			}
			items = append(items, entry)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	limit := normalizeLimit(filter.Limit)
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

package operations

import (
	"context"
	"strings"
	"sync"
	"time"
)

type RingLogSource struct {
	mu       sync.RWMutex
	name     string
	capacity int
	sequence int64
	entries  []LogEntry
}

func NewRingLogSource(name string, capacity int) *RingLogSource {
	if capacity <= 0 {
		capacity = 1000
	}
	return &RingLogSource{name: strings.ToLower(name), capacity: capacity}
}

func (s *RingLogSource) Name() string { return s.name }

func (s *RingLogSource) Append(level, message string, fields map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	entry := LogEntry{
		Cursor:    s.sequence,
		ID:        s.name + ":" + time.Now().UTC().Format("20060102T150405.000000000Z"),
		Source:    s.name,
		Level:     strings.ToLower(level),
		Message:   message,
		Fields:    fields,
		CreatedAt: time.Now().UTC(),
	}
	s.entries = append(s.entries, entry)
	if len(s.entries) > s.capacity {
		copy(s.entries, s.entries[len(s.entries)-s.capacity:])
		s.entries = s.entries[:s.capacity]
	}
}

func (s *RingLogSource) List(ctx context.Context, filter LogFilter) ([]LogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := normalizeLimit(filter.Limit)
	items := make([]LogEntry, 0, limit)
	for _, entry := range s.entries {
		if !matchesFilter(entry, filter) {
			continue
		}
		items = append(items, entry)
		if len(items) > limit {
			items = items[len(items)-limit:]
		}
	}
	return items, nil
}

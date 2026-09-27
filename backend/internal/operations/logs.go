package operations

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrSourceNotFound = errors.New("log source not available")

type LogFilter struct {
	ProjectID   string
	JobID       string
	Level       string
	Search      string
	AfterCursor int64
	Limit       int
}

type LogEntry struct {
	Cursor    int64          `json:"cursor"`
	ID        string         `json:"id"`
	Source    string         `json:"source"`
	ProjectID *string        `json:"project_id,omitempty"`
	JobID     *string        `json:"job_id,omitempty"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type LogSource interface {
	Name() string
	List(ctx context.Context, filter LogFilter) ([]LogEntry, error)
}

type Registry struct {
	mu      sync.RWMutex
	sources map[string]LogSource
}

func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]LogSource)}
}

func (r *Registry) Register(source LogSource) error {
	name := strings.ToLower(strings.TrimSpace(source.Name()))
	if name == "" {
		return errors.New("log source name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sources[name]; exists {
		return errors.New("log source already registered: " + name)
	}
	r.sources[name] = source
	return nil
}

func (r *Registry) Source(name string) (LogSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	source, ok := r.sources[strings.ToLower(strings.TrimSpace(name))]
	return source, ok
}

func (r *Registry) Sources() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.sources))
	for name := range r.sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func matchesFilter(entry LogEntry, filter LogFilter) bool {
	if entry.Cursor <= filter.AfterCursor {
		return false
	}
	if filter.ProjectID != "" && (entry.ProjectID == nil || *entry.ProjectID != filter.ProjectID) {
		return false
	}
	if filter.JobID != "" && (entry.JobID == nil || *entry.JobID != filter.JobID) {
		return false
	}
	if filter.Level != "" && !strings.EqualFold(entry.Level, filter.Level) {
		return false
	}
	if filter.Search != "" && !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(filter.Search)) {
		return false
	}
	return true
}

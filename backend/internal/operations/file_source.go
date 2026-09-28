package operations

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const maxFileLogBytes int64 = 4 << 20

type FileLogSource struct {
	name string
	path string
}

func NewFileLogSource(name, path string) *FileLogSource {
	return &FileLogSource{name: strings.ToLower(strings.TrimSpace(name)), path: strings.TrimSpace(path)}
}

func (s *FileLogSource) Name() string { return s.name }

func (s *FileLogSource) List(ctx context.Context, filter LogFilter) ([]LogEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(s.path)
	if err != nil {
		return nil, fmt.Errorf("open %s log: %w", s.name, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s log: %w", s.name, err)
	}
	start := info.Size() - maxFileLogBytes
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek %s log: %w", s.name, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileLogBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s log: %w", s.name, err)
	}
	lines := strings.Split(string(data), "\n")
	limit := normalizeLimit(filter.Limit)
	out := make([]LogEntry, 0, limit)
	base := info.ModTime().UTC()
	for index, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		created := base.Add(time.Duration(index-len(lines)) * time.Nanosecond)
		entry := LogEntry{
			Cursor:    created.UnixNano(),
			ID:        fmt.Sprintf("%s:%d", s.name, created.UnixNano()),
			Source:    s.name,
			Level:     inferFileLogLevel(line),
			Message:   line,
			CreatedAt: created,
		}
		if !matchesFilter(entry, filter) {
			continue
		}
		out = append(out, entry)
		if len(out) > limit {
			out = out[len(out)-limit:]
		}
	}
	return out, nil
}

func inferFileLogLevel(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error"), strings.Contains(lower, "fatal"), strings.Contains(lower, "critical"):
		return "error"
	case strings.Contains(lower, "warn"):
		return "warn"
	case strings.Contains(lower, "debug"):
		return "debug"
	default:
		return "info"
	}
}

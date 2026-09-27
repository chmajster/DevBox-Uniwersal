package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SQLMode string

const (
	LogModeProject    SQLMode = "project"
	LogModeDeployment SQLMode = "deployment"
	LogModeJob        SQLMode = "job"
)

type SQLLogSource struct {
	name string
	db   *sql.DB
	mode SQLMode
}

func NewSQLLogSource(name string, db *sql.DB, mode SQLMode) *SQLLogSource {
	return &SQLLogSource{name: strings.ToLower(name), db: db, mode: mode}
}

func (s *SQLLogSource) Name() string { return s.name }

func (s *SQLLogSource) List(ctx context.Context, filter LogFilter) ([]LogEntry, error) {
	clauses := []string{"1=1"}
	args := make([]any, 0, 8)

	switch s.mode {
	case LogModeProject:
		clauses = append(clauses, "j.project_id IS NOT NULL")
	case LogModeDeployment:
		clauses = append(clauses, "(j.type LIKE 'deploy%' OR j.type LIKE 'deployment%')")
	case LogModeJob:
	default:
		return nil, fmt.Errorf("unsupported SQL log mode %q", s.mode)
	}

	if filter.ProjectID != "" {
		clauses = append(clauses, "j.project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.JobID != "" {
		clauses = append(clauses, "j.id = ?")
		args = append(args, filter.JobID)
	}
	if filter.Level != "" {
		clauses = append(clauses, "LOWER(jl.level) = LOWER(?)")
		args = append(args, filter.Level)
	}
	if filter.Search != "" {
		clauses = append(clauses, "LOWER(jl.message) LIKE LOWER(?)")
		args = append(args, "%"+filter.Search+"%")
	}
	if filter.AfterCursor > 0 {
		clauses = append(clauses, "jl.id > ?")
		args = append(args, filter.AfterCursor)
	}

	args = append(args, normalizeLimit(filter.Limit))
	query := `SELECT jl.id,j.id,j.project_id,jl.level,jl.message,jl.fields_json,jl.created_at
		FROM job_logs jl
		JOIN jobs j ON j.id = jl.job_id
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY jl.id DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query %s logs: %w", s.name, err)
	}
	defer rows.Close()

	items := make([]LogEntry, 0, normalizeLimit(filter.Limit))
	for rows.Next() {
		var cursor int64
		var jobID string
		var projectID sql.NullString
		var level, message, fieldsJSON, created string
		if err := rows.Scan(&cursor, &jobID, &projectID, &level, &message, &fieldsJSON, &created); err != nil {
			return nil, fmt.Errorf("scan %s log: %w", s.name, err)
		}
		fields := map[string]any{}
		if fieldsJSON != "" {
			_ = json.Unmarshal([]byte(fieldsJSON), &fields)
		}
		entry := LogEntry{
			Cursor:    cursor,
			ID:        fmt.Sprintf("%s:%d", s.name, cursor),
			Source:    s.name,
			JobID:     &jobID,
			Level:     strings.ToLower(level),
			Message:   message,
			Fields:    fields,
			CreatedAt: parseLogTime(created),
		}
		if projectID.Valid {
			entry.ProjectID = &projectID.String
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s logs: %w", s.name, err)
	}

	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	return items, nil
}

func parseLogTime(value string) time.Time {
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

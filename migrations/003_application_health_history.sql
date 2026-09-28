CREATE TABLE health_check_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id TEXT NOT NULL REFERENCES health_checks(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    response_time_ms INTEGER NOT NULL DEFAULT 0,
    message TEXT,
    error TEXT,
    checked_at TEXT NOT NULL
);

CREATE INDEX idx_health_history_project_checked
ON health_check_history(project_id, checked_at DESC);

CREATE INDEX idx_health_history_check_checked
ON health_check_history(check_id, checked_at DESC);

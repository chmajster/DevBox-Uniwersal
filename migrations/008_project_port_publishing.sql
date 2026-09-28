CREATE TABLE project_port_publishing (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    desired_json TEXT NOT NULL,
    applied_json TEXT,
    user_configured INTEGER NOT NULL DEFAULT 0 CHECK (user_configured IN (0, 1)),
    updated_at TEXT NOT NULL
);

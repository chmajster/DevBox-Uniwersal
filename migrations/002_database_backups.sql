CREATE TABLE database_backups (
    id TEXT PRIMARY KEY,
    database_id TEXT NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    file_name TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('queued','ready','failed')),
    size_bytes INTEGER NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    error TEXT,
    created_at TEXT NOT NULL,
    completed_at TEXT
);
CREATE INDEX idx_database_backups_database ON database_backups(database_id, created_at DESC);
CREATE UNIQUE INDEX idx_database_users_username ON database_users(username);

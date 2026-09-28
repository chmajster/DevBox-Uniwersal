CREATE TABLE control_plane_backups (
    id TEXT PRIMARY KEY,
    file_name TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('queued','running','ready','failed','importing','restoring','pending_restore')),
    size_bytes INTEGER NOT NULL DEFAULT 0,
    sha256 TEXT,
    requested_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    error TEXT,
    created_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE INDEX idx_control_plane_backups_created_at
ON control_plane_backups(created_at DESC);

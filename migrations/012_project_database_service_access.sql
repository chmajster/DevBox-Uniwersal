CREATE TABLE project_database_service_access (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    mysql_enabled INTEGER NOT NULL DEFAULT 0 CHECK(mysql_enabled IN (0,1)),
    postgresql_enabled INTEGER NOT NULL DEFAULT 0 CHECK(postgresql_enabled IN (0,1)),
    updated_at TEXT NOT NULL
);

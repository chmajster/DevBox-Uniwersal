CREATE TABLE project_database_bindings (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK(mode IN ('none','managed','compose','external')),
    database_id TEXT REFERENCES databases(id) ON DELETE SET NULL,
    application_service TEXT NOT NULL DEFAULT '',
    compose_service TEXT NOT NULL DEFAULT '',
    engine TEXT NOT NULL DEFAULT 'mysql',
    connection_host TEXT NOT NULL DEFAULT '',
    connection_port INTEGER NOT NULL DEFAULT 3306 CHECK(connection_port BETWEEN 1 AND 65535),
    database_name TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL DEFAULT '',
    secret_ref TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_project_database_bindings_database ON project_database_bindings(database_id);

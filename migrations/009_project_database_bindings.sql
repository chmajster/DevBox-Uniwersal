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

-- Existing per-project databases from releases before database bindings are
-- migrated to managed mode without rotating users or secrets.
INSERT INTO project_database_bindings(
    id, project_id, mode, database_id, application_service, compose_service,
    engine, connection_host, connection_port, database_name, username,
    secret_ref, created_at, updated_at
)
SELECT
    lower(hex(randomblob(16))),
    d.project_id,
    'managed',
    d.id,
    '',
    '',
    d.engine,
    '',
    3306,
    '',
    '',
    NULL,
    datetime('now'),
    datetime('now')
FROM databases d
WHERE d.project_id IS NOT NULL
  AND d.id = (
      SELECT d2.id
      FROM databases d2
      WHERE d2.project_id = d.project_id
      ORDER BY d2.created_at, d2.id
      LIMIT 1
  );

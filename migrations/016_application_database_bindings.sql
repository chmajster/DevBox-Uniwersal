CREATE TABLE application_database_bindings (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    database_id TEXT NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_application_database_bindings_database ON application_database_bindings(database_id);

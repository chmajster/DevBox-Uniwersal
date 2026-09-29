CREATE TABLE git_integrations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    provider TEXT NOT NULL CHECK(provider IN ('github','gitlab')),
    base_url TEXT NOT NULL,
    credential_id TEXT NOT NULL REFERENCES credentials(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_git_integration_credential ON git_integrations(credential_id);

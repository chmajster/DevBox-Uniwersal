CREATE TABLE runtime_installations (
    id TEXT PRIMARY KEY,
    runtime_type TEXT NOT NULL CHECK (runtime_type IN ('php','node','python','go')),
    version TEXT NOT NULL,
    executable_path TEXT NOT NULL DEFAULT '',
    installation_root TEXT NOT NULL DEFAULT '',
    architecture TEXT NOT NULL,
    platform TEXT NOT NULL,
    installation_method TEXT NOT NULL,
    managed_by_devbox INTEGER NOT NULL CHECK (managed_by_devbox IN (0,1)),
    source TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('installed','installing','broken','unavailable','removing','failed')),
    metadata_json TEXT NOT NULL DEFAULT '{}',
    error_text TEXT,
    installed_at TEXT,
    last_validated_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_runtime_installations_type ON runtime_installations(runtime_type, status);
CREATE INDEX idx_runtime_installations_executable ON runtime_installations(executable_path);
CREATE UNIQUE INDEX idx_runtime_managed_version
    ON runtime_installations(runtime_type, version, platform, architecture)
    WHERE managed_by_devbox = 1;
CREATE UNIQUE INDEX idx_runtime_system_executable
    ON runtime_installations(runtime_type, executable_path)
    WHERE managed_by_devbox = 0;

CREATE TABLE runtime_defaults (
    runtime_type TEXT PRIMARY KEY CHECK (runtime_type IN ('php','node','python','go')),
    runtime_installation_id TEXT NOT NULL REFERENCES runtime_installations(id) ON DELETE CASCADE,
    requested_version TEXT NOT NULL,
    updated_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE project_runtime_assignments (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    runtime_type TEXT NOT NULL CHECK (runtime_type IN ('php','node','python','go')),
    runtime_installation_id TEXT NOT NULL REFERENCES runtime_installations(id) ON DELETE RESTRICT,
    requested_version TEXT NOT NULL,
    resolved_version TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(project_id, runtime_type)
);
CREATE INDEX idx_project_runtime_installation ON project_runtime_assignments(runtime_installation_id);

CREATE TABLE source_control_integrations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
    provider TEXT NOT NULL CHECK (provider IN ('github','gitlab')),
    web_url TEXT NOT NULL,
    api_url TEXT NOT NULL,
    credential_id TEXT NOT NULL REFERENCES credentials(id) ON DELETE RESTRICT,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    status TEXT NOT NULL CHECK (status IN ('connected','degraded','authentication_failed','unavailable','disabled')),
    account_username TEXT,
    account_id TEXT,
    scopes_json TEXT NOT NULL DEFAULT '[]',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    last_tested_at TEXT,
    last_synced_at TEXT,
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_source_control_integrations_provider ON source_control_integrations(provider, enabled);
CREATE INDEX idx_source_control_integrations_credential ON source_control_integrations(credential_id);

CREATE TABLE source_control_cache (
    integration_id TEXT NOT NULL REFERENCES source_control_integrations(id) ON DELETE CASCADE,
    cache_kind TEXT NOT NULL,
    cache_key TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(integration_id, cache_kind, cache_key)
);
CREATE INDEX idx_source_control_cache_expiry ON source_control_cache(expires_at);

CREATE TABLE project_source_control (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    integration_id TEXT NOT NULL REFERENCES source_control_integrations(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL CHECK (provider IN ('github','gitlab')),
    repository_external_id TEXT NOT NULL,
    repository_owner TEXT NOT NULL,
    repository_path TEXT NOT NULL,
    repository_name TEXT NOT NULL,
    clone_url TEXT NOT NULL,
    web_url TEXT NOT NULL,
    default_branch TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_project_source_control_integration ON project_source_control(integration_id);

ALTER TABLE projects ADD COLUMN runtime_version TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN container_policy TEXT NOT NULL DEFAULT 'auto' CHECK(container_policy IN ('auto','custom'));

UPDATE projects
SET deployment_mode='docker';

CREATE TABLE project_runtime_modules (
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    runtime TEXT NOT NULL,
    module_name TEXT NOT NULL,
    version_constraint TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(project_id, runtime, module_name)
);
CREATE INDEX idx_project_runtime_modules_project ON project_runtime_modules(project_id);

CREATE TABLE project_runtime_state (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    container_name TEXT NOT NULL,
    image_tag TEXT NOT NULL,
    build_fingerprint TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

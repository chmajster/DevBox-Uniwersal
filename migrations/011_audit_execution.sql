ALTER TABLE projects ADD COLUMN runtime_options_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE jobs ADD COLUMN resource_key TEXT NOT NULL DEFAULT 'global';
UPDATE jobs SET resource_key='project:' || project_id WHERE project_id IS NOT NULL;
CREATE INDEX idx_jobs_resource_status ON jobs(resource_key,status,created_at);
ALTER TABLE project_database_bindings ADD COLUMN database_user_id TEXT REFERENCES database_users(id) ON DELETE RESTRICT;
-- Pin the same deterministic legacy account once, rather than selecting the
-- first remaining account every time credentials are resolved.
UPDATE project_database_bindings SET database_user_id=(
 SELECT u.id FROM database_users u WHERE u.database_id=project_database_bindings.database_id
 ORDER BY u.created_at,u.id LIMIT 1
) WHERE mode='managed';
CREATE INDEX idx_project_binding_user ON project_database_bindings(database_user_id);

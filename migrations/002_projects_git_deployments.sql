ALTER TABLE projects ADD COLUMN source_type TEXT NOT NULL DEFAULT 'empty' CHECK(source_type IN ('git','local','empty'));
ALTER TABLE projects ADD COLUMN local_path TEXT;
ALTER TABLE projects ADD COLUMN runtime TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN deployment_mode TEXT NOT NULL DEFAULT 'native' CHECK(deployment_mode IN ('native','docker'));
ALTER TABLE projects ADD COLUMN working_directory TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN build_command TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN start_command TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN healthcheck TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN auto_start INTEGER NOT NULL DEFAULT 0 CHECK(auto_start IN (0,1));
ALTER TABLE projects ADD COLUMN archived_at TEXT;
ALTER TABLE projects ADD COLUMN current_commit TEXT;

ALTER TABLE project_sources ADD COLUMN credential_kind TEXT;
ALTER TABLE project_sources ADD COLUMN credential_name TEXT;
CREATE UNIQUE INDEX idx_project_sources_project_unique ON project_sources(project_id);

ALTER TABLE deployments ADD COLUMN commit_before TEXT;
ALTER TABLE deployments ADD COLUMN commit_after TEXT;
ALTER TABLE deployments ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE deployments ADD COLUMN current_stage TEXT NOT NULL DEFAULT 'QUEUED';
ALTER TABLE deployments ADD COLUMN error_text TEXT;
ALTER TABLE deployments ADD COLUMN job_id TEXT REFERENCES jobs(id) ON DELETE SET NULL;
CREATE INDEX idx_deployments_job ON deployments(job_id);

ALTER TABLE jobs ADD COLUMN application_id TEXT REFERENCES applications(id) ON DELETE SET NULL;
CREATE INDEX idx_jobs_application_id ON jobs(application_id, created_at DESC);

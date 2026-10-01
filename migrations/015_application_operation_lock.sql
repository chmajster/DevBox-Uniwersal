-- Only one queued/running lifecycle operation may own an application.
CREATE UNIQUE INDEX idx_jobs_application_active
ON jobs(application_id)
WHERE application_id IS NOT NULL AND status IN ('queued', 'running');

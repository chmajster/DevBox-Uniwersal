ALTER TABLE health_checks ADD COLUMN last_response_ms INTEGER;
ALTER TABLE health_checks ADD COLUMN last_error TEXT;

CREATE UNIQUE INDEX idx_health_checks_project_type_target
ON health_checks(project_id, type, target);

-- OCI images and standalone Dockerfile drivers are no longer application modes.
-- Historical records are retained for operators, but new/updated records use
-- the two active drivers and source directories (including Git checkouts).
-- Preserve references before changing the historical schema. No user data is discarded.
CREATE TABLE IF NOT EXISTS legacy_application_images AS SELECT application_id,docker_image FROM application_sources;
ALTER TABLE application_sources DROP COLUMN docker_image;
CREATE TRIGGER hosting_source_insert BEFORE INSERT ON applications
WHEN NEW.source_type NOT IN ('git','local','empty') OR NEW.driver NOT IN ('','managed','compose')
BEGIN SELECT RAISE(ABORT,'unsupported hosting source or runtime mode'); END;
CREATE TRIGGER hosting_source_update BEFORE UPDATE OF source_type,driver ON applications
WHEN NEW.source_type NOT IN ('git','local','empty') OR NEW.driver NOT IN ('','managed','compose')
BEGIN SELECT RAISE(ABORT,'unsupported hosting source or runtime mode'); END;

-- Restore OCI sources within the same Application aggregate and managed driver.
CREATE TABLE IF NOT EXISTS legacy_application_images(application_id TEXT, docker_image TEXT);
ALTER TABLE application_sources ADD COLUMN docker_image TEXT;
UPDATE application_sources SET docker_image=(SELECT docker_image FROM legacy_application_images old WHERE old.application_id=application_sources.application_id);
DROP TRIGGER IF EXISTS hosting_source_insert;
DROP TRIGGER IF EXISTS hosting_source_update;
CREATE TRIGGER hosting_source_insert BEFORE INSERT ON applications
WHEN NEW.source_type NOT IN ('git','local','empty','docker_image') OR NEW.driver NOT IN ('','managed','compose')
BEGIN SELECT RAISE(ABORT,'unsupported hosting source or runtime mode'); END;
UPDATE applications SET source_config_json=json_set(source_config_json,'$.deployment_mode',driver),driver='managed' WHERE driver IN ('dockerfile','image');
CREATE TRIGGER hosting_source_update BEFORE UPDATE OF source_type,driver ON applications
WHEN NEW.source_type NOT IN ('git','local','empty','docker_image') OR NEW.driver NOT IN ('','managed','compose')
BEGIN SELECT RAISE(ABORT,'unsupported hosting source or runtime mode'); END;

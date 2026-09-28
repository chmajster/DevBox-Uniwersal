-- Repair the automatic compatibility backfill introduced by migration 009.
--
-- Migration 009 created managed bindings for databases that already existed on
-- the previously configured host/external MySQL endpoint. No data migration to
-- devbox-mysql happened, so those bindings could silently redirect applications
-- to an empty Docker volume.
--
-- Rows created by migration 009 use SQLite datetime('now') and therefore do not
-- contain the RFC3339 "T" separator used by bindings created through the API.
-- Restrict the cleanup further to the exact shape of the compatibility rows so
-- explicitly configured managed bindings remain untouched.
DELETE FROM project_database_bindings
WHERE mode = 'managed'
  AND database_id IS NOT NULL
  AND application_service = ''
  AND compose_service = ''
  AND connection_host = ''
  AND connection_port = 3306
  AND database_name = ''
  AND username = ''
  AND secret_ref IS NULL
  AND instr(created_at, 'T') = 0;

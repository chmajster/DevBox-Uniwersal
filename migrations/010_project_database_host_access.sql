ALTER TABLE project_database_bindings
ADD COLUMN host_access_only INTEGER NOT NULL DEFAULT 0 CHECK(host_access_only IN (0,1));

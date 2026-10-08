-- Public environment values have their own records; encrypted values remain
-- in application_secret_bindings/SecretStore. Migrate existing configuration.
INSERT INTO application_environment_variables(id, application_id, workload_id, name, value, created_at, updated_at)
SELECT lower(hex(randomblob(16))), a.id, NULL, e.key, CAST(e.value AS TEXT), a.created_at, a.updated_at
FROM applications a, json_each(a.source_config_json, '$.environment') e
WHERE json_type(a.source_config_json, '$.environment')='object';
UPDATE applications SET source_config_json=json_remove(source_config_json,'$.environment');
CREATE UNIQUE INDEX idx_application_environment_variables_global
ON application_environment_variables(application_id,name) WHERE workload_id IS NULL;

ALTER TABLE application_database_bindings ADD COLUMN user_id TEXT REFERENCES database_accounts(id) ON DELETE RESTRICT;
UPDATE application_database_bindings SET user_id=(SELECT user_id FROM database_user_grants WHERE database_id=application_database_bindings.database_id ORDER BY created_at LIMIT 1);

# databases package

Database module owned by Agent 5.

Implemented capabilities:

- MySQL/MariaDB-compatible provider using controlled `mysql` and `mysqldump` invocations.
- database, user and scoped grant lifecycle;
- per-project provisioning with generated credentials stored in `secrets.SecretStore`;
- backup/restore jobs persisted in the shared `jobs` and `job_logs` tables;
- backup download/delete;
- independent phpMyAdmin Docker lifecycle;
- self-contained API module under `/api/v1`.

No HTTP input is executed as an arbitrary shell command. SQL identifiers and privileges are validated before provider execution.

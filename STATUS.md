# Status

Branch: `agent/05-databases`

Agent 5 database scope is implemented on top of the Agent 1 foundation.

Implemented:

- Concrete MySQL provider conforming to `providers.DatabaseProvider`.
- MySQL/MariaDB-compatible status, version and connection checks.
- Managed database create/delete/list/size operations.
- Database user create/delete/password rotation.
- Whitelisted, database-scoped grant/revoke operations; no application receives global grants.
- Per-project database provisioning with a dedicated user and cryptographically generated password.
- Database credentials encrypted through `secrets.SecretStore`; secret references and plaintext are masked from list APIs and job payloads.
- Controlled MySQL CLI execution using a mode-0600 temporary client option file and SQL over stdin; no arbitrary shell endpoint.
- Persistent database backup metadata migration.
- `mysqldump` backup and MySQL restore handlers executed as jobs with shared `jobs`/`job_logs` persistence.
- Backup list/download/delete and restore APIs; backup downloads require Operator or Admin.
- Independent phpMyAdmin Docker install/start/stop/restart/status lifecycle.
- Database API module with Viewer/Operator RBAC, audit events and same-origin checks for mutations.
- Frontend “Bazy danych” page with required database table, project provisioning, backup/restore controls and phpMyAdmin panel.
- Configuration for MySQL CLI/admin connection, backup directory and phpMyAdmin container.
- Tests for identifier validation, secure password generation, database-scoped grants, secret masking, backup job behavior and restore failure handling.
- Job start/cancel transition is atomic at the SQLite status boundary so a cancelled queued backup/restore is not started afterward.

Schema changes:

- `migrations/002_database_backups.sql` adds `database_backups` and a global uniqueness index for managed database usernames.

Shared additive integration change:

- `api.CurrentUser(ctx)` exposes the already-authenticated principal to domain modules for actor-aware audit/job records without exposing the private context key.

Validation:

- GitHub Actions run `36343000426`: successful.
- Backend: `gofmt`, `go vet ./...`, `go test ./...`, `go build ./cmd/devbox` all successful.
- Frontend: `npm ci`, lint, TypeScript typecheck, Vitest and Vite production build all successful.

# Changelog

## Unreleased

### Added

- Initial DevBox Universal repository architecture.
- Go backend bootstrap, environment configuration and graceful HTTP shutdown.
- SQLite bootstrap and ordered SQL migration engine.
- Complete initial schema for users, sessions, projects, project sources, runtime configs, ports, domains, databases, database users, deployments, jobs, job logs, secrets, health checks, audit events and settings.
- Authentication foundation with opaque sessions, bcrypt passwords and Admin/Operator/Viewer RBAC.
- Versioned `/api/v1` endpoints and normalized success/error envelopes.
- Audit event foundation.
- AES-256-GCM encryption and `SecretStore` abstraction.
- Job Engine and Runtime contracts.
- Git, process, Docker, database, reverse proxy, port allocator and system service provider contracts.
- React/TypeScript/Vite frontend shell, login flow and protected layout.
- Reproducible `go.sum` and `package-lock.json`.
- CI for backend and frontend quality gates.
- Architecture, roadmap, agent rules and ADR documentation.
- MySQL/MariaDB-compatible database provider with status/version/connection checks, database lifecycle and database size reporting.
- Managed database users with secure password generation, password rotation and whitelist-based per-database grants/revokes.
- Project database provisioning that creates a dedicated database and user, stores the generated password in encrypted `SecretStore`, and returns the credential only from the provisioning response.
- Database backup metadata and `mysqldump`/restore job handlers integrated with shared jobs/job logs.
- Backup list/download/delete and restore API operations.
- Independent phpMyAdmin Docker container lifecycle and frontend controls, with loopback MySQL translated to Docker host-gateway connectivity.
- “Bazy danych” frontend page with database table, provisioning forms, backup controls, MySQL status and phpMyAdmin status/actions.
- Same-origin protection for database-module browser mutations and actor-aware audit integration.

### Security

- Application projects never receive the MySQL administrative account.
- MySQL application accounts are unique and scoped to their own database rather than `*.*`.
- MySQL SQL is sent to controlled CLI processes over stdin; HTTP clients cannot supply arbitrary shell commands.
- Administrative MySQL credentials are written only to a temporary mode-0600 option file for command execution and are not logged.
- Database passwords and secret references are excluded from list serialization and job payloads.
- SQL backup downloads require Operator or Admin instead of Viewer access.
- Queued job cancellation wins the atomic queued-to-running transition, preventing a cancelled backup/restore from starting afterward.

### Changed

- Added `api.CurrentUser(ctx)` as an additive module integration helper for authenticated actor identity.
- Added MySQL/phpMyAdmin environment configuration to `.env.example`.

### Migration

- `002_database_backups.sql`: adds `database_backups` and a unique managed database username index.

### Validation

- Agent 1 GitHub Actions run `36341280870` passed all backend and frontend foundation quality gates.
- Agent 5 GitHub Actions run `36343217115` passed backend formatting, vet, tests and build plus frontend install, lint, typecheck, tests and production build.

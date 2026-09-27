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
- Docker provider with engine detection and Docker-backed status reporting.
- Container list/inspect/create/start/stop/restart/remove/logs plus allow-listed diagnostic exec.
- Image list/pull/remove/inspect, volume list/inspect/remove and network list/inspect.
- Docker Compose support for all common Compose filenames with config validation, pull, build, up, down, restart, logs and ps.
- Validated Compose discovery under `DEVBOX_PROJECTS_ROOT`.
- Docker API module with Viewer/Operator/Admin RBAC and audit events for privileged actions.
- Docker frontend with Containers, Images, Volumes, Networks and Compose Projects views.
- Docker provider unit tests plus optional integration coverage that skips when Docker is unavailable.

### Security

- Docker HTTP APIs never pass caller-provided raw CLI argument vectors to the Docker CLI.
- No shell is invoked for Docker operations.
- Docker identifiers, image references, Compose project names, service names and project paths are validated before execution.
- Container exec exposes only fixed diagnostic command aliases.

### Validation

- Foundation GitHub Actions run `36341280870` passed all backend and frontend quality gates.
- Agent 4 Docker validation pending final branch CI run.

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
- Runtime registry and concrete Static, PHP, Python, Go and Node.js lifecycle providers.
- Runtime detection for Composer/Laravel/Symfony, requirements/pyproject/Django/FastAPI/Flask, Go modules, Node package/lock files, Vite and static HTML.
- Runtime host availability/version/dependency inspection with `available`, `missing` and `invalid` states.
- PHP-FPM startup configuration and Composer extension validation without implicit system-package installation.
- Per-project Python virtual environments with pip, uv and Poetry workflows.
- Controlled Go build output under `.devbox/build`.
- npm/pnpm/yarn lockfile-aware Node.js install/build/start workflows.
- Loopback-only built-in static HTTP serving for reverse-proxy integration, including deterministic listener shutdown/restart.
- Runtime ProjectContext resolver and SecretStore-backed environment resolution with secret masking.
- Runtime REST API for listing providers, detecting project runtimes, reading project runtime state and validation.
- Runtime Manager UI and reusable project Runtime configuration section.
- Fixture-based runtime detector coverage plus lifecycle/log-tail/secret-masking tests.

### Validation

- Foundation GitHub Actions run `36341280870` passed all backend quality gates: formatting, vet, tests and build.
- Foundation GitHub Actions run `36341280870` passed all frontend quality gates: `npm ci`, lint, typecheck, tests and production build.
- Runtime Engine GitHub Actions run `36343299084` passed backend formatting, vet, tests and build plus frontend install, lint, typecheck, tests and production build.

### Agent 4 — Docker

- Docker provider with engine detection and live Docker status.
- Containers, images, volumes, networks and Docker Compose lifecycle operations.
- Docker/DockerCompose project detection and project-root path validation.
- RBAC/audit-protected Docker API and frontend management page.
- Docker operations use controlled CLI arguments without arbitrary shell execution.

### Agent 5 — Databases

- MySQL/MariaDB provider, database/user/grant lifecycle and per-project provisioning.
- Secure generated application credentials stored through SecretStore.
- Backup/restore jobs and phpMyAdmin container lifecycle.
- Database API, RBAC/audit integration and database management frontend.

### Agent 6 — Networking / Nginx

- Central port allocator with DB and live-socket collision checks.
- Nginx provider with candidate validation, activation, reload and rollback.
- Domains, hosts-file integration and HTTP/TCP health checks.
- Networking API, RBAC/audit integration and Domains/Ports frontend.

### Agent 7 — UI / Operations / Monitoring

- Host CPU/RAM/disk/process monitoring with authenticated snapshot and SSE stream.
- Central log registry with project, deployment, job and DevBox log sources.
- Operations dashboard, applications list, project details, logs viewer and job progress UI.
- Persistent light/dark theme and responsive operations layout.
- Existing Runtime, Docker, Database and Networking routes remain available.

### Agent 2 — Projects / Git / Deployment

- Project CRUD/archive and Git/local/empty project sources.
- Git clone/fetch/pull/checkout/status/history with credential masking and SecretStore integration.
- Durable project jobs and deployment state machine with deployment history.
- Project wizard, project list and Git/deployment detail views under /apps.
- Double-submit CSRF protection for authenticated mutations.

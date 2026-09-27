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
- Host monitoring API for CPU, RAM, disk, uptime and process statistics with SSE updates.
- Central log-source registry with source/project/level/search filters and SSE live tail.
- DevBox, job, project and deployment log sources backed by in-memory structured logs and durable SQLite job logs.
- Dashboard operational metrics and service-state cards.
- Applications list and full project details navigation.
- REST-wired Start/Stop/Restart/Deploy/Open/Logs/Terminal project actions.
- Job detail UI with stage, progress percentage, elapsed time and live durable logs.
- Text + icon operational status badges.
- Persistent light/dark theme and responsive operations UI.
- UI/provider integration contract for parallel project, Docker, database and proxy agents.

### Changed

- Shared API response helpers are now exported so independently owned modules can use the common success/error envelope.
- Dashboard/domain integration failures show the concrete backend error instead of a generic message.

### Validation

- Foundation GitHub Actions run `36341280870` passed all backend quality gates: formatting, vet, tests and build.
- Foundation GitHub Actions run `36341280870` passed all frontend quality gates: `npm ci`, lint, typecheck, tests and production build.
- Agent 7 GitHub Actions run `36342850475` passed backend formatting, vet, tests and build plus frontend install, lint, typecheck, tests and production build.

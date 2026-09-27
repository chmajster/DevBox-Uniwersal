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
- Central SQLite-backed Port Manager with DB and live-socket collision detection, release/reuse and inspection.
- Concrete Nginx provider with detect/version, config generation, candidate validation, site lifecycle and validated reload.
- Transactional-style Nginx activation with file/symlink rollback on failed validation or reload.
- Hostname validation and Windows/WSL/Linux hosts-file reconciliation without privilege escalation.
- HTTP/TCP health checks with persisted status, response time, error and timestamp.
- Networking API for ports, domains, proxy status/test/reload and on-demand health checks.
- `Domeny i Proxy` and `Porty` frontend pages.
- ADR-007 for networking and Nginx safety behavior.

### Changed

- API authentication now exposes authenticated user context to domain modules through `api.CurrentUser` so module-owned mutations can preserve audit actor attribution.
- Runtime configuration accepts the port allocation range, Nginx binary/site paths, hosts-file override and network health timeout.
- Health-check schema stores `last_response_ms` and `last_error`.

### Validation

- Foundation GitHub Actions run `36341280870` passed all backend quality gates: formatting, vet, tests and build.
- Foundation GitHub Actions run `36341280870` passed all frontend quality gates: `npm ci`, lint, typecheck, tests and production build.
- Networking branch adds focused tests for collisions, hostname validation, Nginx rollback, HTTP timeout, TCP checks and hosts-file reconciliation; branch CI runs the full backend and frontend quality gates.

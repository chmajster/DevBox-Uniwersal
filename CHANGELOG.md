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
- Loopback-only built-in static HTTP serving for reverse-proxy integration.
- Runtime ProjectContext resolver and SecretStore-backed environment resolution with secret masking.
- Runtime REST API for listing providers, detecting project runtimes, reading project runtime state and validation.
- Runtime Manager UI and reusable project Runtime configuration section.
- Fixture-based runtime detector coverage.

### Validation

- Foundation GitHub Actions run `36341280870` passed all backend quality gates: formatting, vet, tests and build.
- Foundation GitHub Actions run `36341280870` passed all frontend quality gates: `npm ci`, lint, typecheck, tests and production build.
- Runtime Engine branch CI result will be recorded after final verification.

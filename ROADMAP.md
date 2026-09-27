# Roadmap

## Phase 1 — Foundation

Core Go API, SQLite migrations, authentication/RBAC, audit, encrypted-secret abstraction, job/runtime/provider contracts, frontend shell and CI.

## Phase 2 — Parallel domain implementations

- Git/Projects: source discovery, clone/pull/checkout and project lifecycle.
- Runtimes: detection and lifecycle implementations for PHP, Go, Python, Node and other supported stacks.
- Docker: local Docker engine integration and container lifecycle.
- Databases: MySQL bootstrap, database/user lifecycle and credential references.
- Reverse proxy: Nginx route management, validation and reload.
- Windows/WSL: environment discovery, privilege-separated system services and filesystem/path translation.
- Frontend: project onboarding, module pages and job progress UX.

## Phase 3 — Operations

Monitoring/health history, durable workers, restart recovery, structured streaming logs, port conflict reconciliation, reverse-proxy reconciliation, backups and update workflow.

## Phase 4 — Hardening

CSRF strategy for state-changing cookie-authenticated endpoints, login rate limiting, secret-key rotation, policy expansion, signed releases, installer/updater, end-to-end tests and security review.

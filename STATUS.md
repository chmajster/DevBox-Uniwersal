# Status

DevBox Universal is an integrated local development control plane.

## Core

- Go REST API, SQLite migrations, opaque server-side sessions, Admin/Operator/Viewer RBAC, CSRF protection and audit events.
- AES-256-GCM secret storage abstraction.
- Durable Job Engine with project/deployment job logs.
- React/TypeScript/Vite web UI served by the Go process in production.
- OpenAPI 3.1 discovery at `/api/v1/openapi.json` and authenticated API index at `/api/v1/docs`.

## Workspace UI

- Selected dark operator-console layout with compact grouped sidebar, host/version information and API-backed component health.
- Dashboard CPU/RAM/disk circular gauges, service states, recent application/job tables, filtered log preview and session-history chart with metric/range selection.
- Serialized, abortable polling: inventory/statuses every 30 seconds, dashboard metrics every 10 seconds, dashboard logs every 5 seconds; hidden-tab suspension and explicit unavailable/error states.
- Session charts retain up to two hours / 721 real samples; no synthetic history, network counters or temperature readings. See `docs/control-room-ui.md` for exact semantics.
- Persistent compact sidebar, mobile drawer and role-aware Ctrl/Cmd+K navigation/application search.
- Searchable applications in persistent card/table views, status filtering, deployment queue feedback and archive confirmation.
- Dashboard links select active jobs, job details and full log-source filters; existing SSE views remain available.
- Redesigned login, persistent dark/light theme and shared module forms, tables, statuses and logs.
- Native accessible dialogs, keyboard navigation, visible focus, reduced-motion support and graceful handling of disabled browser storage.
- Frontend unit tests and a Chromium smoke workflow with synthetic API fixtures and screenshot artifacts. This is not full provider/infrastructure E2E coverage.
- Existing API contracts, RBAC, CSRF and backend behavior are unchanged; no migrations or new frontend runtime dependencies.

## Projects / Git / deployments

- Git, local-directory and empty-project onboarding.
- Project CRUD/archive, Git fetch/pull/checkout/history and credential masking.
- Operator-only local-directory browser constrained to configured roots, with canonical symlink handling, traversal limits and audit events.
- Deployment state machine using the shared runtime registry.
- Project-level application health configuration.

## Runtimes

- Static, PHP, Python, Go and Node.js providers.
- Laravel, Symfony, Django, FastAPI, Flask, Vite and generic project detection.
- Python per-project virtual environments, controlled Go build output and lockfile-aware Node package-manager selection.
- Runtime validation, lifecycle control, logs and health checks.

## Docker

- Docker Engine inventory and container lifecycle.
- Images, volumes, networks and Docker Compose project management.
- RBAC/audit protection for mutations.
- Docker output available in central operations logs.

## Databases

- MySQL/MariaDB status, database/user/grant lifecycle and per-project provisioning.
- SecretStore-backed generated credentials.
- Database backup/restore jobs and phpMyAdmin lifecycle.
- Admin-only full control-plane backup/import/download/restore workflow with SQLite snapshot, checksum, encrypted-secret state, managed Nginx files and controlled Docker/Compose manifests. Restore is validated and applied before database open on the next service start.

## Networking

- SQLite-backed port allocator with live socket checks.
- Nginx domains/reverse proxy with validate → activate → validate → reload and rollback.
- Hosts-file integration and HTTP/TCP checks.

## Monitoring / operations

- Host CPU/RAM/disk/process monitoring with snapshot and SSE.
- Scheduled project HTTP/TCP health checks, persisted current state and historical retention.
- Central logs for DevBox, jobs, projects, deployments and Docker; optional Nginx/MySQL file sources.
- Aggregate `all` log stream, search/level/project/time filters and text export.

## Windows / WSL / installer

- Windows PowerShell bootstrap with WSL distribution detection and Linux handoff.
- Linux installer with install/status/repair/update/uninstall/help.
- `devbox status`, `devbox doctor` and allowlisted privileged helper.

## Current hardening work

- Login rate limiting and session administration.
- Secret-key rotation/versioning.
- Signed release artifacts and rollback-capable updater.
- Full browser E2E coverage.
- Runtime version installation/isolation.
- Automatic local HTTPS.

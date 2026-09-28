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
- Runtime/deployment API and persistence now use the managed-container model; migration 007 adds per-project runtime versions, container policy, module selections and managed image state.

## Projects / Git / deployments

- Git, local-directory and empty-project onboarding.
- Project CRUD/archive, Git fetch/pull/checkout/history and credential masking.
- Operator-only local-directory browser constrained to configured roots, with canonical symlink handling, traversal limits and audit events.
- Deployment state machine uses Docker as the mandatory application execution boundary; project Compose/Dockerfile definitions take precedence, otherwise DevBox builds a managed runtime image.
- Deploy from the applications list opens the application's Deployments tab immediately; application details show the live stage, refresh it every second through completion and keep the last success/failure visible; concurrent Deploy clicks are blocked while one is active.
- Project-level application health configuration.

## Runtimes

- Static, PHP, Python, Go and Node.js project detection remains available without requiring those runtimes on the host.
- Application runtimes execute in Docker; native host execution is no longer a supported deployment mode.
- Per-project runtime version and allowlisted image modules are persisted and editable from the application configuration.
- PHP managed images support selectable extensions including PDO MySQL, MySQLi, mbstring, intl, GD, cURL, ZIP, BCMath, OPcache, XML, SOAP, sockets, PCNTL and EXIF.
- Node.js, Python and Go managed images support controlled build dependencies while application dependencies continue to come from package-lock/pnpm/yarn, requirements/pyproject and go.mod.
- Generated images are keyed by deterministic build fingerprints; unchanged images are reused and explicit rebuilds are supported.
- Dedicated `Runtime containers` GitHub Actions workflow verifies backend tests/vet/build and frontend lint/typecheck/tests/build for managed-container changes.

## Docker

- Docker Engine inventory and container lifecycle.
- Images, volumes, networks and Docker Compose project management.
- Managed application image generation for projects without Compose/Dockerfile.
- Atomic managed-container replacement with rollback, no-new-privileges and reduced capabilities.
- Managed build contexts exclude secret environment files and common local dependency/cache directories.
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
- Automatic local HTTPS.

## Configurable project port publishing

- Project configuration now separates the container listener from the published host port. New managed applications start at HTTP 8080; optional TLS passthrough starts at HTTPS 8443.
- The central allocator skips database and socket collisions one port at a time, recognizes reused owned leases, and persists resolved host ports across redeploys/restarts.
- Generated HTTP runtime listeners and image fingerprints follow internal-port changes. Host-port-only changes do not invalidate images or remove live source mounts.
- Optional Compose port management uses an external, durable `!override` file for the selected web service. It is enabled by saving project port configuration and requires Docker Compose 2.24.4+. Unconfigured Compose projects retain their original topology and legacy compatibility.
- Tests cover validation, concurrent allocation, cancellation, IPv4/IPv6 collisions, ownership-aware rollback, persistence, primary HTTP selection, UI fields and opt-in real Docker publication/live mounts/Compose remapping.
- HTTPS certificate provisioning, firewall/NAT/WSL forwarding, and changing a custom application's listener remain separate concerns. See `docs/project-port-publishing.md` and ADR-010.

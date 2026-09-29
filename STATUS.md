# Status

DevBox Universal is an integrated local development control plane.

## Core

- Admin-only control-plane user management supports account creation, Admin/Operator/Viewer role changes, enable/disable, password change/generation, per-user session revocation and account deletion with last-admin/self-protection safeguards.
- Go REST API, SQLite migrations, opaque server-side sessions, Admin/Operator/Viewer RBAC, CSRF protection and audit events.
- AES-256-GCM secret storage abstraction.
- Durable Job Engine with project/deployment job logs.
- React/TypeScript/Vite web UI served by the Go process in production.
- OpenAPI 3.1 discovery at `/api/v1/openapi.json` and authenticated API index at `/api/v1/docs`.

## Workspace UI

- Recovered the approved logo from the valid favicon frame, replaced the truncated inline PNG with a bundled binary asset, and moved the unchanged ICO into the asset pipeline; integrity and production-browser regression checks cover both themes and mobile navigation. See `docs/branding.md` for provenance and test scope.
- Selected dark operator-console layout with compact grouped sidebar, host/version information and API-backed component health.
- Dashboard CPU/RAM/disk circular gauges, service states, recent application/job tables, filtered log preview and session-history chart with metric/range selection.
- Serialized, abortable polling: inventory/statuses every 30 seconds, dashboard metrics every 10 seconds, dashboard logs every 5 seconds; hidden-tab suspension and explicit unavailable/error states.
- Session charts retain up to two hours / 721 real samples; no synthetic history, network counters or temperature readings. See `docs/control-room-ui.md` for exact semantics.
- Persistent compact sidebar, mobile drawer and role-aware Ctrl/Cmd+K navigation/application search.
- Searchable applications in persistent card/table views, status filtering, deployment queue feedback and archive confirmation.
- Project Overview exposes the active HTTP application address as a direct new-tab link using the DevBox browser host and the project's resolved host port.
- Dashboard links select active jobs, job details and full log-source filters; existing SSE views remain available.
- Redesigned login, persistent dark/light theme and shared module forms, tables, statuses and logs.
- Native accessible dialogs, keyboard navigation, visible focus, reduced-motion support and graceful handling of disabled browser storage.
- Frontend unit tests and a Chromium smoke workflow with synthetic API fixtures and screenshot artifacts. This is not full provider/infrastructure E2E coverage.
- Runtime/deployment API and persistence now use the managed-container model; migration 007 adds per-project runtime versions, container policy, module selections and managed image state.

## Projects / Git / deployments

- Git, local-directory and empty-project onboarding; local Git worktrees on WSL/Windows mounts use per-command `safe.directory` trust so DevBox can read repositories owned by the interactive Windows user without changing global Git configuration.
- Project CRUD/archive, Git fetch/pull/checkout/history and credential masking.
- Operator-only local-directory browser constrained to configured roots, with canonical symlink handling, traversal limits and audit events.
- Deployment state machine uses Docker as the mandatory application execution boundary; project Compose/Dockerfile definitions take precedence, otherwise DevBox builds a managed runtime image.
- Deployment has a dedicated database-resolution stage that resolves project/runtime environment, waits for managed MySQL when required, injects database credentials at runtime and runs real connectivity checks before activation.
- Deploy from the applications list opens the application's Deployments tab immediately; application details show the live stage, refresh it every second through completion and keep the last success/failure visible; concurrent Deploy clicks are blocked while one is active.
- Project-level application health configuration.

## Runtimes

- Static, PHP, Python, Go and Node.js project detection remains available without requiring those runtimes on the host.
- Application runtimes execute in Docker; native host execution is no longer a supported deployment mode.
- Host PHP-FPM is optional and is labelled as such in Plugins; managed PHP applications use the PHP runtime built into their Docker image rather than host PHP-FPM.
- Per-project runtime version and allowlisted image modules are persisted and editable from the application configuration.
- PHP managed images expose per-project module selection directly on the application Overview and Runtime configuration, including projects whose PHP runtime was auto-detected. Selections are persisted per project, participate in the managed-image fingerprint and are installed on the next managed deployment. Available container extensions include PDO MySQL, MySQLi, PostgreSQL, SQLite3, mbstring, intl, GD, Imagick, cURL, ZIP, BCMath, GMP, OPcache, XML, SOAP, LDAP, Redis, Memcached, Xdebug, sockets, PCNTL and EXIF.
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

- Project database mode selection uses a dedicated responsive five-option layout. `Połącz z bazą na hoście` is a network-access-only choice: it stores no database name, username or password, does not inject database credential variables, and shows the exact `host.docker.internal:<port>` application endpoint.
- Per-project database bindings support `none`, DevBox-managed MySQL, project-owned Compose MySQL/MariaDB, external credentialed MySQL/MariaDB and host-access-only MySQL/MariaDB/PostgreSQL.
- Host database access discovers installed MySQL/MariaDB and PostgreSQL instances, including PostgreSQL cluster ports from `pg_lsclusters`. The API also returns non-loopback host interface IPv4/IPv6 addresses, and the project UI shows them with the selected database port as alternatives next to `host.docker.internal`. Selecting an instance sets its engine/port automatically; custom ports remain editable. The persisted host-access-only choice still stores only `host.docker.internal`, the selected engine/port and the `host_access_only` flag. External credentialed loopback bindings still normalize to the same Docker host target.
- External and Compose bindings can explicitly represent an empty database password in SecretStore; omitting the password continues to preserve an existing secret instead of silently replacing it.
- Managed MySQL runs as `devbox-mysql` on the shared `devbox-apps` network with persistent `devbox-mysql-data`, loopback-only admin publication and durable lifecycle jobs.
- Admin and application endpoints are separate: control-plane operations use the loopback endpoint while application containers use `devbox-mysql:3306`.
- The Plugins page exposes MySQL/MariaDB and PostgreSQL as two independent plugin entries, consistent with Docker Compose, PHP-FPM and phpMyAdmin. Each entry represents a shared database server, not one database: one installed server can host many logical databases and users, and many applications may use that server concurrently. Each server has its own install/status action through the durable Job Engine and is discovered automatically by project host-database bindings.
- Host SQL services are application infrastructure only; DevBox Universal keeps its own control-plane state in SQLite. Managed `devbox-mysql` keeps the application endpoint `devbox-mysql:3306`, while its loopback administrative publication uses the dedicated configurable `DEVBOX_MANAGED_MYSQL_ADMIN_PORT` (default `13306`). This leaves host TCP 3306 available for the MySQL/MariaDB plugin unless another real listener occupies it. phpMyAdmin remains reconciled with host-gateway access and reports host-MySQL reachability.
- Existing database/user/grant provisioning is reused; database-user passwords may be explicitly empty or securely generated when omitted. Newly created or rotated users return complete application connection settings (`DB_HOST`, `DB_PORT`, database, username, password), and legacy loopback MySQL endpoints are translated to `host.docker.internal` for container use. Generated credentials and external/Compose passwords remain SecretStore-backed and are injected only at runtime.
- phpMyAdmin is reconciled against the current configured MySQL target before it is opened, so a stale running phpMyAdmin container does not keep an obsolete `PMA_HOST`/`PMA_PORT`.
- Project Compose files remain untouched; DevBox creates a mode-0600 override outside the repository for environment/network additions and detects or explicitly selects the application service.
- Connection tests execute real `SELECT 1`; managed tests traverse Docker DNS on `devbox-apps`, Compose tests use the selected database service and external tests run from the Docker execution boundary.
- Generated PHP runtimes validate the selected database family: MySQL/MariaDB host access requires `pdo_mysql` or `mysqli`, while PostgreSQL host access requires `pgsql`. The project database UI can add the matching module before rebuild.
- Database backup/restore jobs remain unchanged and the project database tab exposes assigned backups; phpMyAdmin uses `devbox-mysql` over `devbox-apps` in managed mode.
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

- Git/systemd updates expose persistent 0–100% progress and concrete stages from source download through backend/frontend builds, artifact installation, service update, healthcheck and final restart; the Updates page resumes polling after the expected DevBox service restart and, after a tracked successful update, performs a one-time cache-busting page reload before removing the temporary refresh marker from the URL.
- Windows PowerShell bootstrap with WSL distribution detection and Linux handoff.
- Linux installer with install/status/repair/update/uninstall/help; clean installations default to Docker-managed MySQL and install only the host client, while existing pre-binding host-MySQL installations remain legacy unless explicitly migrated.
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

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
- Workspace application badges and status filtering use live Docker ownership/state; a running managed/Compose deployment overrides stale persisted project errors, with stopped/failed/unhealthy container states reflected directly.
- Project Overview exposes the active HTTP application address as a direct new-tab link using the DevBox browser host and the project's resolved host port.
- Application Settings allow operators to change the selected runtime/technology and the source directory of local-source applications; path changes use backend directory validation and Git/empty managed paths stay read-only.
- Project Ports uses a compact host-to-container mapping table with inline TCP type, automatic internal HTTP port detection, optional HTTPS passthrough and the existing save/deploy workflow.
- Dashboard links select active jobs, job details and full log-source filters; existing SSE views remain available.
- Redesigned login, persistent dark/light theme and shared module forms, tables, statuses and logs.
- Native accessible dialogs, keyboard navigation, visible focus, reduced-motion support and graceful handling of disabled browser storage.
- Frontend unit tests and a Chromium smoke workflow with synthetic API fixtures and screenshot artifacts. This is not full provider/infrastructure E2E coverage.
- Update failures expose the exact installer/updater stage, exit code, sanitized recent updater log lines and the most relevant failure line directly in the Updates UI; secrets matching common credential patterns are redacted before API delivery.
- Runtime/deployment API and persistence now use the managed-container model; migration 007 adds per-project runtime versions, container policy, module selections and managed image state.

## Projects / Git / deployments

- Git, local-directory and empty-project onboarding; local Git worktrees on WSL/Windows mounts use per-command `safe.directory` trust so DevBox can read repositories owned by the interactive Windows user without changing global Git configuration.
- Project CRUD/archive, Git fetch/pull/checkout/history and credential masking.
- Operator-only local-directory browser constrained to configured roots, with canonical symlink handling, traversal limits and audit events.
- Directory selection uses one synchronized path input/tree component: backend filesystem autocomplete is debounced and capped at 20 results, opening the tree lazily expands the current path and scrolls to the selected directory, and all browsing remains constrained to allowlisted roots with symlink/traversal protections.
- Directory tree template literals are valid TypeScript/JSX and covered by the normal frontend lint/typecheck/build gates.
- Deployment state machine uses Docker as the mandatory application execution boundary; project Compose/Dockerfile definitions take precedence, otherwise DevBox builds a managed runtime image.
- Deployment has a dedicated database-resolution stage that resolves project/runtime environment, waits for managed MySQL when required, injects database credentials at runtime and runs real connectivity checks before activation.
- Deploy from the applications list opens the application's Deployments tab immediately; application details show the live stage, refresh it every second through completion and keep the last success/failure visible; concurrent Deploy clicks are blocked while one is active.
- Project-level application health configuration.

## Runtimes

- Static, PHP, Python, Go and Node.js project detection remains available without requiring those runtimes on the host.
- Application runtimes execute in Docker; native host execution is no longer a supported deployment mode.
- Host PHP-FPM is optional and is labelled as such in Plugins; managed PHP applications use the PHP runtime built into their Docker image rather than host PHP-FPM.
- Per-project runtime version and allowlisted image modules are persisted and editable from the application configuration.
- Managed PHP deployments automatically translate supported Composer `ext-*` requirements from both `require` and `require-dev` into the same allowlisted image-module catalog; manual module selections remain additive.
- PHP managed images expose per-project module selection directly on the application Overview and Runtime configuration, including projects whose PHP runtime was auto-detected. Selections are persisted per project, participate in the managed-image fingerprint and are installed on the next managed deployment. Available container extensions include PDO MySQL, MySQLi, PostgreSQL, SQLite3, mbstring, intl, GD, Imagick, cURL, ZIP, BCMath, GMP, OPcache, XML, SOAP, LDAP, Redis, Memcached, Xdebug, sockets, PCNTL and EXIF.
- Node.js, Python and Go managed images support controlled build dependencies while application dependencies continue to come from package-lock/pnpm/yarn, requirements/pyproject and go.mod.
- Generated images are keyed by deterministic build fingerprints; unchanged images are reused and explicit rebuilds are supported.
- Dedicated `Runtime containers` GitHub Actions workflow verifies backend tests/vet/build and frontend lint/typecheck/tests/build for managed-container changes.

## Docker

- Docker Engine inventory and container lifecycle.
- Images, volumes, networks and Docker Compose project management.
- Docker Containers are grouped into expandable application cards using real Compose/DevBox ownership metadata; project services such as `plan-web-1` and `plan-db-1` stay together, DevBox infrastructure has its own card, and per-container lifecycle/log actions remain available inside each group.
- Managed application image generation for projects without Compose/Dockerfile.
- Managed image builds prefer Docker BuildKit/buildx with plain build output and retain the legacy image-builder fallback only when buildx is unavailable; bounded Docker failures preserve the final actionable error lines.
- Managed image/container names are normalized to Docker-safe repository components, including legacy project IDs; malformed references are rejected before invoking Docker and image-inspection failures are recorded in the dependency/runtime deployment stage.
- Atomic managed-container replacement with rollback, no-new-privileges and reduced capabilities.
- Managed build contexts exclude secret environment files and common local dependency/cache directories.
- RBAC/audit protection for mutations.
- Docker output available in central operations logs.
- Every DevBox application container is attached to the shared `devbox-apps` network. This applies to generated runtimes, custom Dockerfile deployments and all running services of project-owned Compose deployments, so they can resolve plugin database containers by Docker DNS.

## Databases

- Project database mode selection keeps no-database, shared DevBox services, project Compose and external-server workflows. In the shared DevBox mode the application can select MySQL/MariaDB, PostgreSQL or both; the project screen shows only non-editable Docker DNS/port/network/status data and does not expose database/user/password/grant forms.
- Shared-service choices are persisted independently in `project_database_service_access`. MySQL/MariaDB uses `devbox-mysql:3306`, PostgreSQL uses `devbox-postgresql:5432`, and both use `devbox-apps`. Database creation, SQL accounts, passwords and per-database grants remain managed in the Databases module.
- The backend still accepts previously persisted `host_access_only` bindings and explicit external loopback targets for compatibility, but new project UI configuration does not create host-access-only bindings or auto-discover SQL packages/services on the host.
- External and Compose bindings can explicitly represent an empty database password in SecretStore; omitting the password continues to preserve an existing secret instead of silently replacing it.
- MySQL/MariaDB and PostgreSQL are installed from Plugins as persistent Docker database servers. MySQL uses `devbox-mysql` + `devbox-mysql-data`; PostgreSQL uses `devbox-postgresql` + `devbox-postgresql-data`. Both join the shared `devbox-apps` network and use `unless-stopped` restart policy.
- Admin and application endpoints are separate: control-plane operations use the loopback endpoint while application containers use `devbox-mysql:3306`.
- The Plugins page exposes MySQL/MariaDB and PostgreSQL as two independent Docker-backed plugin entries. Installation pulls the configured image, creates the persistent volume, creates/reuses the shared application network and starts the database container through the durable Job Engine; it no longer installs either database server as a host package.
- DevBox Universal keeps its control-plane state in SQLite. Plugin database servers are application infrastructure: containers use Docker DNS `devbox-mysql:3306` and `devbox-postgresql:5432`. MySQL keeps a loopback-only control-plane admin publication on `DEVBOX_MANAGED_MYSQL_ADMIN_PORT` (default `13306`); PostgreSQL is not published to the host by default.
- Existing database/user/grant provisioning is reused; database-user passwords may be explicitly empty or securely generated when omitted. Newly created or rotated users return complete application connection settings (`DB_HOST`, `DB_PORT`, database, username, password), and legacy loopback MySQL endpoints are translated to `host.docker.internal` for container use. Generated credentials and external/Compose passwords remain SecretStore-backed and are injected only at runtime.
- SQL users are server-level accounts. One MySQL/MariaDB or PostgreSQL account can be assigned to multiple managed databases on the same server, with an independent privilege set for each database. The engine Users tab opens a dedicated account page for password changes, database assignment/removal and per-database grants.
- phpMyAdmin is reconciled against the current MySQL target before it is opened. In the standard managed setup it joins `devbox-apps` and connects directly to `devbox-mysql:3306`; it no longer adds host MySQL as a second automatic target or performs host-MySQL reachability checks.
- Project Compose files remain untouched; DevBox creates a mode-0600 override outside the repository for environment/network additions and detects or explicitly selects the application service.
- Credential-bearing binding tests execute real `SELECT 1`; legacy managed tests traverse Docker DNS on `devbox-apps`, Compose tests use the selected database service and external tests run from the Docker execution boundary. The credential-free shared-service selector intentionally does not manufacture an account just to test connectivity.
- Generated PHP runtimes surface missing database drivers for the selected shared services: MySQL/MariaDB can add `pdo_mysql`, while PostgreSQL can add `pgsql`/PDO PostgreSQL through the existing runtime module mechanism.
- Database backup/restore jobs and phpMyAdmin remain managed in the central database/plugin workflows; the shared-service project view no longer duplicates account/password/backup administration.
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

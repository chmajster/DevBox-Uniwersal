# Changelog

## 2026-09-29 — Managed image reference hardening

### Fixed

- normalized legacy project identifiers before composing managed/custom Docker image and container names so separator runs cannot produce Docker-invalid repository references,
- rejected project identifiers that cannot produce a safe Docker resource suffix before invoking the Docker CLI,
- tightened image-reference validation for repository components, tags, registries and digests so malformed references fail as DevBox input errors rather than opaque Docker daemon `invalid reference format` failures,
- moved managed-image state/inspection into the dependency stage and included the generated image reference in the safe error context, so deployment history no longer mislabels image-inspection failures as database configuration failures.

### Tests

- added regression coverage for legacy project IDs, custom Dockerfile empty identifiers, Docker reference grammar and persisted deployment stage on managed-image inspection failure.

## Unreleased

### Docker container organization

- changed the Docker Containers tab from one flat table to expandable application cards,
- Compose containers are grouped by the real `com.docker.compose.project` label and managed DevBox containers by `io.devbox.project`, avoiding unreliable name-prefix guessing,
- DevBox-owned infrastructure containers are grouped separately and unassigned containers remain visible in a fallback card,
- each card summarizes running/stopped counts while preserving Logs/Start/Stop/Restart for every individual container,
- added backend parsing and frontend grouping regressions for Compose projects, managed applications, infrastructure and independent containers.


### Directory picker parser fix

- Removed accidental escape characters from JSX/TypeScript template literals in `DirectoryPicker.tsx`, restoring ESLint parsing and frontend compilation after the tree-view UI merge.

### Directory picker UI

- Rebuilt the project directory chooser into an Explorer-style folder tree with expandable chevrons, yellow folder icons and a compact selected-row treatment.
- Kept the existing allowlisted-root API, lazy directory loading, cycle protection, traversal limit messages, double-click selection and final path confirmation.
- Added a frontend regression test for the new tree shell.



### Port management UI

- Rebuilt the per-project Docker port editor into a compact table matching Docker-style port mapping: host port, container port and TCP type are visible in one row.
- HTTP keeps automatic internal-port detection through an Auto field, while HTTPS is added or removed as a second mapping row without changing the existing backend contract.
- Applied mappings use the same compact table layout, and Compose service selection plus save/deploy semantics remain unchanged.


### Project detail navigation

- Split the application detail configuration into dedicated top-level tabs for Runtime, Baza danych, Porty and Ustawienia instead of stacking all configuration panels under one page.
- Reworked the application navigation into a continuous responsive tab strip with a clear active indicator and horizontal scrolling on narrow screens; legacy `?tab=configuration` links now open Ustawienia.
- Runtime no longer duplicates the port panel when rendered inside application details, while the standalone runtime page keeps its previous combined behavior.

### Live updater progress

- After a tracked update reaches `succeeded`, the Updates page performs a one-time cache-busting navigation so the freshly installed SPA document and hashed frontend assets are loaded instead of leaving the pre-update UI in memory. The temporary refresh query parameter is removed from browser history immediately after the new page loads.
- Added persistent live update state with percentage, current stage, source/target versions and timestamps, exposed through `GET /api/v1/update/progress`.
- The updater now records source checks, clone/validation, all eight installer stages, final restart, success/no-update and failure states in `/var/lib/devbox/update-status`.
- The Updates UI polls only the lightweight local progress endpoint during execution, survives the expected API restart, renders a 0–100% progress bar and marks every real updater stage as pending/current/completed/failed/skipped.
- Installer stage reporting remains a no-op outside updater-driven `--update` runs, so normal install/repair/status behavior is unchanged.
- Failed updater runs now preserve the latest installer stage/percentage, store the process exit code, and return a bounded sanitized tail of `/var/log/devbox-update.log` through the admin-only progress API.
- The Updates failure panel now shows the technical stage, exit code, log path, exact relevant failure line and an expanded recent-log console instead of only telling the operator to inspect the log file manually.

### Control-plane user management

- Added admin-only user management for DevBox accounts: create accounts, assign Admin/Operator/Viewer roles, enable or disable accounts, change or generate passwords, revoke all user sessions and delete accounts.
- Password changes revoke all sessions for the affected account. Role/status changes also invalidate sessions so permission changes take effect immediately.
- Added safeguards against deleting, disabling or demoting the current administrator and against removing the final active administrator account.
- Added an admin-only `Użytkownicy` page and documented `/api/v1/users` management endpoints.

### Unified project database connectivity

- Reworked the project-level DevBox database choice so it no longer asks for database names, SQL users or passwords. It now selects MySQL/MariaDB, PostgreSQL or both and displays only fixed application-facing Docker DNS, port, network and runtime status.
- Added persisted `project_database_service_access` state and authenticated GET/PUT project APIs for shared SQL service selection. SQL databases, accounts, passwords and grants remain exclusively managed from the Databases module; selecting a service does not provision or rotate credentials.
- Reworked SQL users into server-level accounts: the MySQL/MariaDB and PostgreSQL Users tabs now list accounts, each account opens on its own page, and one account can receive access to multiple databases with an independent privilege set per database.
- Added account-wide password change/generation plus database assignment/removal APIs and migrated existing one-database user rows into `database_accounts` and `database_user_grants` without dropping existing credentials or grants.
- Simplified the project database UI after moving SQL plugins to Docker: removed the dedicated host-database discovery/access mode, host IP/service/version fields and host-specific connection guide. Managed applications now see the real Docker endpoint `devbox-mysql:3306` and network `devbox-apps`.
- Removed the obsolete `GET /api/v1/plugins/databases/host` discovery endpoint and its host package/service probes. Existing persisted `host_access_only` bindings remain backend-compatible, but the project UI no longer creates new ones.
- phpMyAdmin now targets the managed MySQL container directly on `devbox-apps` and reports that Docker target/network. It no longer adds host MySQL as an automatic second target or displays host-gateway/reachability state.
- Changed the separate `MySQL / MariaDB` and `PostgreSQL` Plugin entries to install persistent Docker database servers instead of host packages. MySQL uses `devbox-mysql:3306`; PostgreSQL uses `devbox-postgresql:5432`; both use persistent named volumes and the shared `devbox-apps` network. MySQLi remains a PHP driver, not a database server.
- Kept host SQL strictly application-facing: the DevBox control plane continues to use SQLite. Moved the managed `devbox-mysql` loopback administrative publication to configurable `DEVBOX_MANAGED_MYSQL_ADMIN_PORT` (default `13306`) while preserving `devbox-mysql:3306` inside Docker, so host MySQL/MariaDB can use TCP 3306 simultaneously.
- Added managed-MySQL reconciliation labels so upgrades recreate only the `devbox-mysql` container when its managed configuration changes while preserving `devbox-mysql-data` and the SecretStore root credential. The privileged helper sudoers policy now permits the allowlisted MySQL/MariaDB/PostgreSQL service restarts required after package installation.
- Fixed database-user connection details so newly created or rotated credentials include the actual application-facing `DB_HOST`, `DB_PORT`, database and username; loopback control-plane MySQL endpoints are normalized to `host.docker.internal` for application containers.
- Opening phpMyAdmin now reconciles its container/database target first, avoiding stale `PMA_HOST`/`PMA_PORT` configuration after database topology changes.
- Host MySQL/MariaDB bindings now work from managed containers, custom Dockerfile deployments and project Compose: loopback hosts (`127.0.0.1`, `localhost`, `::1`) resolve to `host.docker.internal`, and DevBox injects `host.docker.internal:host-gateway` where Linux/WSL Docker needs it.
- External/Compose database bindings can explicitly store an empty password in SecretStore; the project UI exposes a `Użytkownik bazy nie ma hasła` option without conflating it with preserving an existing secret.
- Database status now exposes both the application-facing MySQL/MariaDB address (`devbox-mysql:3306` for managed mode) and the loopback administrative endpoint; the Databases UI shows the host, port, Docker network and ready-to-copy `DB_HOST` value.
- Added managed PostgreSQL as `devbox-postgresql` (`postgres:17` by default) with persistent `devbox-postgresql-data`, SecretStore-backed admin password and no default host port publication.
- Added migration `009_project_database_bindings.sql` with `none`, `managed`, `compose` and `external` project database modes plus backfill for existing per-project databases, and migration `010_project_database_host_access.sql` for the credential-free host-access flag.
- Split database administration and application endpoints so host control-plane operations can use loopback while containers use Docker DNS `devbox-mysql:3306`.
- Added a persistent managed MySQL 8.4 container on `devbox-apps` with `devbox-mysql-data`, loopback-only admin publishing, SecretStore root credentials, restart reconciliation and durable lifecycle jobs.
- Attached every DevBox application container to the shared application network: generated/custom Docker containers join it before start, and all running services in project-owned Compose deployments are connected after `compose up`. Plugin DB containers are therefore reachable from application containers by stable Docker DNS without host gateway routing.
- Added runtime SecretStore/environment merging with database binding variables at highest precedence for generated runtimes, project Dockerfiles and project Compose.
- Added private mode-0600 Compose overrides outside application repositories, application-service detection/selection and support for both project `db` service DNS and managed `devbox-mysql`.
- Added real `SELECT 1` connection testing from the Docker execution boundary, secret-safe Docker env handling, PHP MySQL-driver validation and project database UI/actions.
- Kept existing provisioning, scoped grants, backups/restores, phpMyAdmin, RBAC and audit behavior; phpMyAdmin now reconciles onto the managed MySQL network.
- Database server installation from Plugins is Docker-only and no longer requires privileged-helper package/service permissions. Legacy persisted host-access bindings remain readable, while new configuration uses managed Docker, Compose or explicit external endpoints.
- Added unit/integration coverage plus a Docker E2E that provisions a real database, builds PHP with `pdo_mysql` and validates `SELECT 1` over Docker networking.
- Added ADR-011 documenting binding resolution, endpoint separation, environment precedence, Compose override security and WSL behavior.

### Configurable Docker port publishing

- Added a clickable application address to the project Overview, built from the DevBox host used in the browser and the project's active HTTP port; the link opens the running application in a new tab.
- Added project HTTP/internal and published-port settings, optional HTTPS passthrough, high defaults (8080/8443), sequential collision fallback and durable resolved-port persistence.
- Kept exact manual port reservations unchanged; added ownership-aware allocation/cleanup and fixed primary-port selection so HTTPS does not replace HTTP.
- Added a separate configuration UI with validation, save/deploy actions, applied mappings and application links.
- Added managed-listener reconfiguration, multi-port Docker publication and opt-in Compose overrides stored outside application repositories.
- Added migration `008_project_port_publishing.sql`, shared optional provider contracts, ADR-010 and regression/integration tests.

### Brand image rendering fix

- Replaced the truncated hand-copied PNG data URL with the intact 32x32 frame extracted from the existing approved favicon; the sidebar, login and host-summary logo consumers retain their layout.
- Stored the original ICO and recovered PNG as binary assets emitted with content-hashed, same-origin URLs rather than inline base64 strings.
- Added five standard-library asset-integrity tests and Chromium production-bundle decoding checks for both themes, collapsed sidebar and mobile drawer, including a same-origin-only image CSP.
- Documented asset provenance and native resolution in `docs/branding.md`. No backend contract, database migration or runtime dependency changes.

### Managed runtime containers

- PHP projects now expose a dedicated per-project deployment-module section directly on the application overview (and runtime views), including auto-detected PHP projects; selected extensions are persisted to the runtime configuration and can be saved for the next deploy or saved and deployed immediately.
- Moved PHP module selection out of the global Plugins page into each project's Runtime configuration; the selector is shown only when PHP is selected and selected extensions are built into that project's managed PHP container.
- Expanded the managed PHP container catalog with PostgreSQL, SQLite3, LDAP, GMP, Imagick, Redis, Memcached and Xdebug in addition to the existing PHP extensions; PECL-backed extensions are installed and enabled inside the image.
- Removed native host runtime deployment for applications. PHP, Node.js, Python, Go and static projects now execute through Docker.
- Added automatic managed image generation when an application does not provide its own Compose file or Dockerfile; project-owned container definitions take precedence.
- Added per-application runtime version, container policy and allowlisted runtime module configuration with dedicated API and UI.
- Added selectable PHP extensions and controlled build dependencies for Node.js, Python and Go without installing project runtimes or modules globally on the host.
- Added migration 007 for runtime versions, container policy, project module selections and persisted managed image/container fingerprints.
- Added deterministic image fingerprints and explicit rebuild support so unchanged images can be reused.
- Added sanitized managed build contexts that exclude .env files, VCS metadata, dependency trees and common local caches.
- Added atomic managed-container replacement with health verification and rollback to the previous container on failure.
- Deploy now switches directly to a live deployment view showing the current stage and progress until success/failure; failures preserve the exact stage where execution stopped.
- Project runtime validation no longer requires PHP-FPM, Go, Node.js or Python executables on the host; Plugins now marks host PHP-FPM as optional because managed PHP applications use their container runtime.
- Removed the unnecessary Docker Compose `--project-directory` flag; Compose now uses the absolute `--file` path and works with Docker installations that reject that flag.
- Added a dedicated Runtime containers CI workflow covering managed-container backend and frontend quality gates.


### Selected control-room UI

- Implemented the selected compact dark DevOps dashboard: circular CPU/RAM/disk gauges, service status, application/job tables, terminal-style log preview and utilization chart.
- Connected all panels to existing APIs with abortable serialized polling, hidden-tab suspension, timeout handling and explicit unavailable states instead of fictional metrics.
- Added bounded session telemetry history with metric/range selection, timestamp ordering and gap handling; CPU temperature remains unavailable because the API does not provide it.
- Added API-backed component health and host/version sidebar summary, searchable authorized views/applications and a dark-by-default persistent design preference.
- Added active-job and selected-job deep links and log-source navigation from dashboard to full log view; preserved project mutations, CSRF and existing SSE views.
- Extended unit/browser coverage for charts, filters, role visibility, API failures, null collections and 900/390/320-pixel layouts. See `docs/control-room-ui.md` for integration and test scope.
- Applied formatting-only cleanup to three existing Go files (backup repository, configuration, privileged helper) to unblock the inherited backend formatting gate; no backend logic or migration changes.

### Workspace UI redesign

- Replaced the basic shell with grouped, icon-based navigation, persistent compact sidebar, mobile drawer and role-aware Ctrl/Cmd+K navigation search.
- Redesigned the dashboard into application/resource summaries, CPU/RAM/disk meters, service states and application shortcuts; unavailable API data is no longer presented as zero.
- Added searchable card/table application views with persistent view preference, status filtering, deployment queue feedback and archive confirmation.
- Redesigned the login page and unified light/dark tokens across shared module forms, tables, statuses and log viewers; no new frontend runtime dependencies or external assets.
- Added native modal focus handling, visible keyboard focus, reduced-motion support and graceful operation when browser preference storage is blocked.
- Added navigation/filter unit coverage and a Chromium smoke workflow producing screenshots and a report. Browser fixtures are synthetic; real Docker, database, proxy and deployment integration testing remains a separate requirement.
- No backend API contract or database migration changes.

### Fixed

- Database-user editing now opens in a modal dialog instead of expanding below the list; password changes show an immediate green success or red failure message inside the dialog.
- Database users can now intentionally use an empty MySQL/MariaDB password; omitted password fields still request secure automatic generation, and the UI exposes generation as a separate explicit option.
- Local-directory Git repositories opened from WSL/Windows mounts are now trusted only for the current Git command with `safe.directory`, avoiding false `Git repository is not available` errors when the DevBox service account differs from the filesystem owner.
- Git-tab provider errors are now shown inside the Git tab and Fetch/Pull actions are hidden until a valid repository with a remote is available.
- Hardened the local-directory browser against symlink escapes and recursive symlink cycles; traversal is restricted to configured roots and capped at 500 entries.
- Reconciled STATUS/TODO documentation with the integrated implementation so completed foundation work is no longer presented as outstanding.
- Consolidated the duplicate Applications/Aplikacje navigation onto the canonical `/apps` workflow and kept `/applications` as a compatibility redirect.
- Normalized null collection payloads so empty project lists no longer crash with an `items is null` frontend error.
- Replaced hard-coded dark module panel backgrounds with theme variables, fixing unreadable forms in light mode.
- Moved installed DevBox Nginx site state under `/var/lib/devbox/nginx` and delegated global Nginx validation/reload to the allowlisted privileged helper, avoiding unprivileged `nginx -t` failures on root-only includes.

### Added

- Admin-only control-plane backup subsystem with consistent SQLite snapshots, SHA-256 integrity, import/download, staged restore-on-restart, Nginx state and controlled Docker/Compose configuration capture. The master encryption key is deliberately excluded.
- Operator-only local-directory tree browser with configurable roots via `DEVBOX_DIRECTORY_BROWSE_ROOTS` and audited browse activity.
- Scheduled application HTTP/TCP health monitoring with current state, persisted history and configurable retention.
- Aggregate central log source with live SSE tail, time/search/level/project filters, text export and Docker log ingestion.
- Optional Nginx/MySQL file log sources configured explicitly by path.
- OpenAPI 3.1 contract at `/api/v1/openapi.json` and authenticated API index at `/api/v1/docs`.
- MIT license.
- Initial DevBox Universal repository architecture.
- Go backend bootstrap, environment configuration and graceful HTTP shutdown.
- Complete initial schema for users, sessions, projects, project sources, runtime configs, ports, domains, databases, database users, deployments, jobs, job logs, secrets, health checks, audit events and settings.
- Authentication foundation with opaque sessions, bcrypt passwords and Admin/Operator/Viewer RBAC.
- Versioned `/api/v1` endpoints and normalized success/error envelopes.
- Audit event foundation.
- AES-256-GCM encryption and `SecretStore` abstraction.
- Job Engine and Runtime contracts.
- Git, process, Docker, database, reverse-proxy, port and system service provider contracts.
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

### Agent 8 — Windows / WSL / Installer

- Windows/WSL bootstrap installer (`install.ps1`) with distribution detection, optional Ubuntu installation, systemd handling and Linux-installer handoff.
- Linux installer (`install.sh`) with install/status/repair/update/uninstall/help modes, staged logging and data-preserving uninstall by default.
- Host component and platform detection for Git, Docker, Nginx, MySQL/MariaDB, PHP, Composer, Python, pip, Go, Node.js and npm.
- Authenticated `/api/v1/system/components` and `/api/v1/system/platform` status endpoints.
- `devbox status` and `devbox doctor` operator commands.
- Whitelisted `devbox-helper` privilege boundary without arbitrary command execution.
- Production SPA serving from the Go process through `DEVBOX_FRONTEND_DIR`.
- Shell/PowerShell installer tests and CI validation.

Security:
- privileged helper package/service/config operations are allowlisted;
- bootstrap credentials are not written to installer logs;
- uninstall preserves application data unless purge is explicitly requested.

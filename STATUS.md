# Current status

Updated on 2026-10-13; verification from previous sessions is recorded below. Application is the only user-application lifecycle model. `/apps` supports local, Git, empty starter and OCI sources, with existing Compose, existing Dockerfile and generated Docker choices. All application processes run in Docker; the managed and Compose engines share persistent topology, jobs, ports, secrets and reconciliation.

The installed systemd service also deployed the existing OpenWiki Windows source through the DevBox API and Job Engine on port 8080, using PHP 8.4.26 and its existing MySQL database. Login and `/health` returned HTTP 200; the non-root Apache worker (1000:1000) authenticated with application credentials and executed SELECT 1. Host-to-container reads, container-to-host source writes and storage writes passed. The source probe now tolerates DrvFS chmod rejection while still proving actual reads/writes. Full backend tests, vet, Docker race tests, backend build, the real non-root DrvFS regression and the real PHP writable-mount test passed. External local sources require the specific systemd ReadWritePaths exception documented in the installer guide. Browser UI automation was unavailable in this session; this deployment used the actual API and job lifecycle.

Implemented:

- Five-step onboarding, backend runtime/version catalog, official-image availability checks and automatic rebuild on a provisioned application's version change.
- PHP/Apache with Composer, document root and extensions; Python framework/dependency detection; Node npm/pnpm/yarn; Go module download, binary build and cmd/ entrypoints; static Nginx and WordPress.
- Actual RW code mounts, dependency volumes, source-owner processes/Apache workers, Dockerfile WORKDIR handling and Linux/Windows-backed WSL mapping.
- Independent MySQL, MariaDB and PostgreSQL servers, persistent volumes, exact application database/account bindings, encrypted password generation, scoped grants and shared Docker DNS networking for web/API/worker/scheduler.
- Application-native SQL provisioning/server jobs, detach and authenticated SELECT 1 from the actual selected application container through the installed static probe. Legacy project SQL mutation routes are retired; historical records remain readable.
- Durable automatic host ports from 8080, host/Docker NAT conflict checks, actual HTTP readiness distinct from container state, live logs/history, CPU/RAM and runtime artifact export.
- Safe managed replacement with readiness checked after rollback; previous-plan recovery for failed Compose command/port updates. Source-preserving deletion protects shared SQL and persistent data.
- RBAC, encrypted SecretStore, redacted logs, source/Compose/build path validation, restricted container capabilities and ownership checks before replacement/removal.
- Linux/Windows Reinstall preserving configuration, SQLite, keys, sources and SQL volumes; Docker/Compose/network diagnostics and sibling probe installation.

## Executed verification on 2026-10-08

Environment: Windows host, Ubuntu-26.04 WSL, Go 1.26.0 and Node 22.22.1. Real containers ran on the native WSL Docker Engine with its Compose plugin. Tests use isolated SQLite databases, uniquely owned containers/networks/volumes and disposable source directories.

| Check | Result |
| --- | --- |
| Full backend `go test ./...`, `go vet ./...` | PASS |
| Race tests for applications, all drivers, Docker, databases, proxy and jobs | PASS |
| Backend, helper and static SQL-probe builds | PASS |
| Frontend tests | 76 PASS in 19 files |
| ESLint, TypeScript and production frontend build | PASS |
| Bash syntax, installer preservation/idempotency tests | PASS |
| ShellCheck 0.11.0 for installer/dev/updater/test scripts | PASS |
| Windows PowerShell syntax and installer metadata/mode tests | PASS |
| Python branding checks | 5 PASS |
| Chromium branding and workspace smoke with production bundle / synthetic API | PASS in GitHub Actions; six branding cases, five-step wizard, configuration, secrets, logs, jobs, Viewer and mobile layouts |
| Docker application lifecycle, all runtimes/WordPress/Compose variants | PASS, 874.02 seconds |
| Docker PHP version isolation and failed replacement rollback | PASS after correction, 84.10 seconds |
| Docker shared MySQL/PostgreSQL/MariaDB and Compose web/worker SQL | PASS, 291.36 seconds |
| Docker sources, errors/retry, private static directory and Windows `/mnt/c` mount | PASS after correction, 302.59 seconds |
| Additional real Docker publications and legacy database connectivity | PASS |
| Additional real Apache non-root worker RW mount test | PASS, 28.06 seconds |
| Running backend HTTP API: catalog, frontend route, create, PostgreSQL provision, deploy, HTTP, SELECT 1, stats, export and source-safe removal | PASS |
| `git diff --check` | PASS |

The first aggregate Docker run exposed premature readiness after rollback and restrictive empty-starter permissions. Both were corrected and their affected groups rerun successfully. A temporary port collision came from overlapping test processes with separate control-plane databases; the final source/recovery run passed after that overlap ended. All four application Docker test groups have passing executed results; these results combine the aggregate run and targeted reruns, rather than claiming an untouched aggregate run passed.

GitHub Actions also confirmed backend/frontend/installer checks, the Runtime containers workflow and [Workspace UI](https://github.com/chmajster/DevBox-Uniwersal/actions/runs/37795894849). Existing browser fixtures were updated to the five-stage workflow, and the source selector received an explicit accessible name. The Dockerfile fixture uses its real source owner so the same RW assertion works on root WSL and non-root CI runners.

Commands, API semantics and the CODEX_TASK A–G mapping are in [Application hosting](docs/application-control-plane.md). CI builds the probe and runs the full Docker application suite with a 50-minute timeout.

## Executed verification on 2026-10-11

The wizard now preserves explicit language/version/mode and edited fields across analysis. Detection returns mixed-runtime candidates, frameworks and real Python/Go entrypoints. Generated Go supports optional watching; Python supports framework reload and live src-layout imports. PHP public-only sources are recognized and module/document-root changes queue rebuilds. Every source deployment checks bidirectional daemon mapping before replacement. Dockerfile/Compose checks use the official alpine:3.22 helper. Root-created private Git/starter sources use UID/GID 10001; local sources and linked targets are not changed.

Environment: Windows host, Ubuntu-26.04 WSL, Docker Engine 29.1.3, Compose 2.40.3, Go 1.26.0, Node 22.22.1, Python 3.14 and headless Playwright 1.63 Chromium. Disposable resources and separate port ranges isolate the test groups from existing applications.

| Check | Result |
| --- | --- |
| gofmt, full backend tests, go vet | PASS |
| Race tests: applications, drivers, Docker, databases, proxy and jobs | PASS |
| Backend, helper and static SQL-probe builds | PASS |
| Frontend unit tests | 77 PASS in 19 files |
| ESLint, TypeScript and production build | PASS |
| Bash syntax, ShellCheck, installer preservation/idempotency | PASS |
| Native Windows PowerShell syntax and installer tests | PASS |
| Python branding checks / Chromium branding | 5 / 6 PASS |
| Local Chromium workspace smoke, production bundle / synthetic API | PASS; language before directory, version/mode/port/command/environment retention, lifecycle/jobs/secrets, RBAC and mobile layouts |
| Real Docker lifecycle: generated runtimes, WordPress and Compose filenames | PASS, 953.06 s; Compose variants after helper-probe change PASS, 187.02 s |
| Real Docker concurrent PHP versions and failed replacement rollback | PASS, 90.12 s |
| Real Docker Python 3.12/3.13 and Go 1.25/1.26, concurrent isolation, nested/multiple entrypoints, RW, manual rebuild and reload | PASS after fixture correction, 331.17 s; final Go watcher rerun PASS, 194.40 s |
| Real Docker MySQL/PostgreSQL/MariaDB, account isolation, persisted data and Compose web/worker SQL | PASS, 346.29 s |
| Real Docker sources/recovery, including Windows /mnt/c and Dockerfile mapping probe | PASS after ownership correction, 222.70 s; Git/empty sources also prove runtime-user writes |
| Real Docker legacy database connectivity / publications | PASS, 33.39 / 18.01 s |
| Real Apache non-root worker, public/index.php only, RW and source preflight | PASS, 29.20 s |
| Read-only service source and wrong daemon mapping regressions | PASS; permission check also run as WSL UID 1000 |
| git diff --check | PASS |

The initial aggregate Docker run exposed a nested-file fixture error and root-created Git/starter source ownership that prevented non-root writes. Both were corrected and the affected groups rerun successfully. A targeted Dockerfile run collided with a simultaneously running source group using the same host ports; the final full source/recovery group passed without that overlap. All five application Docker groups have passing executed results. Results combine aggregate and affected-group reruns rather than claiming the initial aggregate passed. The browser fixture also removed a synthetic application before its reads finished; cleanup ordering and normal 404 behavior were corrected and the workspace smoke rerun successfully.

## Executed verification on 2026-10-13

Application details now expose Zmień nazwę and save name/description independently of the runtime form. Existing PATCH semantics preserve the slug, runtime configuration, source, container and published port; metadata changes do not enqueue deployment jobs. The separate runtime form cannot overwrite name/description.

- Full backend tests, go vet and all command builds: PASS.
- Metadata API race tests: PASS; blank/duplicate name failures preserve the original application.
- Frontend: 77 tests, ESLint, TypeScript and production build PASS. Vite retains its non-fatal large-chunk warning.
- Chromium workspace smoke: PASS, including rename with a missing runtime version and an incomplete unsaved runtime form, trimming, duplicate-name feedback, retained edits during polling, reload/list persistence, Operator editing, Viewer restrictions and mobile layouts.
- Real Docker `TestApplicationDockerRename`: PASS, 13.90 seconds; unchanged actual container ID and HTTP response, stable configuration/topology and no extra jobs.
- Linux installer preservation tests and shell syntax; Windows parser/metadata tests: PASS.
- Installed WSL DevBox frontend updated and verified over HTTP against the tested bundle; service and health remained active/OK. Existing application records were not renamed during verification.

The Docker check above is targeted to renaming; the earlier full runtime/database suites were not repeated for this UI change.

The follow-up details UI refinement separates container state, HTTP readiness, runtime, port, database and latest deployment, adds address copying and Polish URL-backed tabs, and refines runtime/topology/resources/logs/settings plus dark/light/mobile layouts. Current runtime metadata overrides the latest successful plan; failed plans and missing legacy metadata do not imply a running configuration. The route is lazy-loaded, removing the previous main-bundle size warning (481.28 kB main JS, 40.53 kB details JS).

- Frontend: 79 tests in 20 files, ESLint, TypeScript and production build PASS, without Vite chunk-size warnings.
- Chromium branding: 6 cases PASS; Python asset integrity: 5 tests PASS.
- Chromium workspace smoke and dedicated application details regression: PASS with the production bundle and synthetic API. Coverage includes independent rename/settings/secrets, RBAC, failed inspection, sections, copy/clipboard, menu Escape/outside dismissal, URL reload/back/forward, deployment vs live runtime, both themes and 900/390/320 px layouts.
- Full backend tests, go vet, command builds and Linux/Windows installer checks: PASS again; backend behavior is unchanged by this refinement.
- Screenshots inspected for the captured read-only Plan application data and synthetic desktop/settings/light/mobile views. Connected browser surfaces were unavailable; this is an isolated visual preview, not a claim of live browser testing against the user's application.

- Installed frontend index, all six generated assets and a direct Settings URL match the tested build over HTTP. The systemd service stayed active, health returned OK, and Plan remained running with unchanged source/configuration, container identity and ports. No existing application was renamed, deployed or restarted during this UI verification.

## Operational limits and unverified items

- Browser regressions use synthetic API fixtures and the production frontend bundle. The current bundle was exercised locally in headless Chromium, with screenshots inspected. Real runtime/SQL behavior was checked separately with Docker; the 2026-10-08 session also checked the backend HTTP API.
- No installed production service was reinstalled or purged. Reinstall preservation was checked through installer tests; real application/SQL persistence was checked with Docker. Repository validation does not replace a host installation trial.
- Domain/TLS routing was covered by backend tests, not a live public DNS/certificate deployment. Nginx/client DNS and existing certificates require operator configuration. ACME remains disabled.
- Compose recovery is best effort for retained images and the previous plan. Source Compose changes, external services and data writes cannot be rolled back atomically; same-port replacement can briefly interrupt service.
- Dockerfile/Compose sources retain responsibility for their own image/process compatibility. Unsupported host privileges and paths outside the authorized application source are rejected. Root-owned/unwritable Linux source directories require suitable operator permissions; DevBox does not make them globally writable.
- Go supports manual rebuilding, compile-on-start and optional container-local polling/rebuilding. A separate minimal production image remains optional and is not implemented. HTTP readiness establishes reachability, not business correctness.
- Windows/WSL mounting was tested against this native WSL daemon. A different Docker Desktop distribution/path integration still requires its own daemon access and mapping checks.

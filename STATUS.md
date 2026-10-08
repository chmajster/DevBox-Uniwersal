# Current status

Verified on 2026-10-08. Application is the only user-application lifecycle model. `/apps` supports local, Git, empty starter and OCI sources, with existing Compose, existing Dockerfile and generated Docker choices. All application processes run in Docker; the managed and Compose engines share persistent topology, jobs, ports, secrets and reconciliation.

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

## Executed verification

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
| Docker application lifecycle, all runtimes/WordPress/Compose variants | PASS, 874.02 seconds |
| Docker PHP version isolation and failed replacement rollback | PASS after correction, 84.10 seconds |
| Docker shared MySQL/PostgreSQL/MariaDB and Compose web/worker SQL | PASS, 291.36 seconds |
| Docker sources, errors/retry, private static directory and Windows `/mnt/c` mount | PASS after correction, 302.59 seconds |
| Additional real Docker publications and legacy database connectivity | PASS |
| Additional real Apache non-root worker RW mount test | PASS, 28.06 seconds |
| Running backend HTTP API: catalog, frontend route, create, PostgreSQL provision, deploy, HTTP, SELECT 1, stats, export and source-safe removal | PASS |
| `git diff --check` | PASS |

The first aggregate Docker run exposed premature readiness after rollback and restrictive empty-starter permissions. Both were corrected and their affected groups rerun successfully. A temporary port collision came from overlapping test processes with separate control-plane databases; the final source/recovery run passed after that overlap ended. All four application Docker test groups have passing executed results; these results combine the aggregate run and targeted reruns, rather than claiming an untouched aggregate run passed.

Commands, API semantics and the CODEX_TASK A–G mapping are in [Application hosting](docs/application-control-plane.md). CI builds the probe and runs the full Docker application suite with a 50-minute timeout.

## Operational limits and unverified items

- Visual wizard/browser interaction was not verified: this session exposes no browser surface. Frontend contracts, lint/typecheck/build and the actual HTTP API passed.
- No installed production service was reinstalled or purged. Reinstall preservation was checked through installer tests; real application/SQL persistence was checked with Docker. Repository validation does not replace a host installation trial.
- Domain/TLS routing was covered by backend tests, not a live public DNS/certificate deployment. Nginx/client DNS and existing certificates require operator configuration. ACME remains disabled.
- Compose recovery is best effort for retained images and the previous plan. Source Compose changes, external services and data writes cannot be rolled back atomically; same-port replacement can briefly interrupt service.
- Dockerfile/Compose sources retain responsibility for their own image/process compatibility. Unsupported host privileges and paths outside the authorized application source are rejected. Root-owned/unwritable Linux source directories require suitable operator permissions; DevBox does not make them globally writable.
- Go supports manual rebuilding and compile-on-start, without automatic file watching or a separate minimal production image. HTTP readiness establishes reachability, not business correctness.
- Windows/WSL mounting was tested against this native WSL daemon. A different Docker Desktop distribution/path integration still requires its own daemon access and mapping checks.

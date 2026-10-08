# Architecture

DevBox Universal is a local hosting control plane with a Go backend, React frontend, SQLite persistence, durable jobs and Docker as the boundary for every user application. Application language runtimes are never started as host processes.

## Responsibilities

| Component | Location | Responsibility |
| --- | --- | --- |
| Application Service/API | `backend/internal/applications` | Validate sources/settings, own desired state, enqueue operations, store topology/history and coordinate managers |
| Runtime Catalog | `backend/internal/containerspec/runtime_catalog.go` | Single version/default catalog consumed by API, detection and frontend |
| Runtime Detector | `backend/internal/runtimes`, application selector | Pure source inspection: Compose/Dockerfile/manifests/frameworks/entry points |
| Managed Runtime Manager | `internal/drivers/managed`, `internal/containerspec`, `internal/docker/runtime_managed.go` | Generate selected runtime, live RW source mounts, UID/GID, private network, safe image/container replacement |
| Compose Manager | `internal/drivers/compose`, `internal/docker/compose*.go` | Preserve original stack, discover services, choose primary, apply private overrides and execute Compose |
| Port Manager | `internal/applications/ports.go`, `internal/proxy/port_manager.go` | Durable endpoint leases and shared reservation space, conflict/socket detection, central inventory |
| Reverse Proxy / SSL Manager | `internal/proxy`, application routing | Domain ownership, certificate validation, candidate/global Nginx validation and controlled reload |
| Environment / Secret Manager | Application repository, `internal/secrets` | Separate public-env table; encrypted secret bindings, injection/redaction |
| Job Manager | `internal/jobs`, application job handlers | Persisted queue, stages/output, cancellation/retry, per-app operation lock |
| Health / Reconciliation Manager | App reconciler and Docker inspections | Actual container/service states, publication checks, stable startup, optional HTTP/source healthchecks |
| Directory browser | `internal/projects/browser.go` | Independently mounted audited browser/create-directory routes with the same configured source roots |

Host Docker/database/plugin inventories, SQL database user administration, backups, monitoring, update handling, audit/RBAC, Windows/WSL setup and narrowly scoped privileged helpers remain independent modules. They do not introduce another application deployment engine.

## Three source execution modes

The application API is `/api/v1/applications`; the canonical frontend route is `/apps`. Exactly two registered drivers exist: `managed` and `compose`. Public settings select `auto` (generated Docker), `dockerfile` or `compose`. OCI sources use mode `image` through the same managed driver. Mode changes apply at the next successful deployment; the current driver owns existing resources until replacement.

Existing Compose detects any supported root Compose filename and retains its normalized services, dependencies and volumes. Ambiguous HTTP service selection requires explicit `compose_service` and container port. Private overrides publish the selected primary endpoint, apply settings/ownership labels, deterministic container names and writable source mounts. Named data volumes are preserved on down/delete. External networks/volumes stay source-owned external resources.

Managed generation covers PHP, Node, Python, Go, static HTML and WordPress as a PHP/Apache profile. Code is mounted RW from the original directory; dependency images/volumes do not replace the live source. Writable processes use the source UID/GID when practical. Managed replacement can restore the old container on failure and preserves the owned port lease. Failed Compose updates reapply the previous successful command/port plan using retained images and volumes. Changes made by the user to the original Compose file or external services cannot be rolled back atomically.

## Persistence and orchestration

Applications own source, settings, desired/observed state, runtime, workloads, endpoints, volumes, secret/env bindings, routes, port leases, deployments and jobs. Hosting read models expose source path, mode/type/version, command, ports, domain, SSL and deterministic Docker project name from these records. Public environment lives in its own table; API settings combine it for editing. Migration 017 separates public environment values. Migration 018 preserves historical image references before its schema change; 019 restores OCI support and maps the old image/Dockerfile drivers into `managed`; 020 persists the exact selected SQL account for each application binding.

Generated artifacts are `<database directory>/projects/<id>/runtime/{Dockerfile,compose.yaml,metadata.json}` with private permissions. They contain no secret values. Durable Compose port overrides default to `<database directory>/compose-ports`; source files are not rewritten. Generated Managed Compose is a reviewable configuration snapshot; actual execution uses the shared Docker provider's replacement mechanism.

HTTP mutations return queued jobs. One job owns an application's operation lock; cancellation retains the lock until cleanup completes. Deployment stages cover source analysis, detection, planning, runtime/dependencies, build, create/start, health, routing and finalization. Rebuild bypasses a cached Managed image and forces Compose recreation; restart retains images. Jobs expose progress, process streams, times and errors without logging resolved Docker introspection env.

Docker names contain immutable application IDs. Ownership labels identify containers/images/private networks and nonexternal Compose networks/volumes. Reconciliation inspects actual resources every five seconds and skips application replacements in progress. It invalidates observations when Docker is unavailable and treats missing/exited desired-running resources and lost publication as failures. Endpoint/domain URLs reflect publication/route activity.

Domains reserve unique ownership before Nginx activation. Candidate and global config pass validation before reload. Existing certificates are verified for domain/date/key pair and terminate TLS at Nginx; application runtime remains HTTP. ACME is disabled because no issuer/challenge implementation is configured.

## Security and deletion

Local source paths are absolute, allowlisted and symlink-resolved; traversal, credential segments and protected system roots are rejected. Start command is parsed into argv; host CLI calls never concatenate a user shell command. Ports, Docker identifiers, runtime versions/modules and domains are validated. Secrets are encrypted, injected separately and masked in application/job output.

Application deletion removes owned containers/networks, generated config, routes, secrets and port reservations; it preserves user source and persistent named volumes. Volume destruction is a separate explicit Docker inventory operation. Go/Node used by the source installer compile DevBox itself; PHP/Composer/Python runtime installation and host-runtime lifecycle endpoints have been removed.

See [Application hosting](docs/application-control-plane.md) for the full workflow, examples, runtime limitations and the A–G Docker verification matrix.

## Shared SQL servers

The Application aggregate owns database/account bindings. Independent MySQL, MariaDB and PostgreSQL managers own their servers and persistent volumes. Provisioning and SQL tests use the durable Job Engine and the application operation lock. Creating a binding generates an application password in SecretStore, grants only the selected database and injects DB_HOST/PORT/NAME/USER/PASSWORD into web/API/worker/scheduler workloads. Infrastructure services do not receive those credentials. All app workloads join the shared Docker DNS network alongside private app networks.

SQL administration runs through clients inside the server containers, so application use does not require host database ports or host SQL clients. PostgreSQL databases revoke PUBLIC connection/creation grants before application grants are applied. The static `devbox-dbcheck` executable is copied into the selected running application container; credentials pass through stdin, and it authenticates and executes SELECT 1. Tests cannot pass merely because a sidecar can reach the database.

Deleting an application detaches its binding and removes only labelled application resources. Shared servers, SQL accounts, databases and durable volumes remain separately administered. Legacy project data is preserved; the Application workflow replaces project-specific database onboarding.

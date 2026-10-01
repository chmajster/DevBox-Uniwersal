# Architecture

## Objective

DevBox Universal is a local application management control plane. The architecture separates stable orchestration contracts from replaceable platform implementations so multiple agents can develop Git, runtime, Docker, database, proxy, monitoring and Windows/WSL capabilities in parallel.

## Repository layout

```text
backend/
  cmd/devbox/              composition root, server bootstrap and operator CLI
  cmd/devbox-helper/       narrow Linux privileged helper
  internal/api/            core API envelope, middleware, core handlers, Module contract
  internal/auth/           authentication/session service
  internal/config/         environment configuration
  internal/database/       SQLite bootstrap and migration runner
  internal/domain/         shared core entities only
  internal/repository/     core repository interfaces + SQLite adapters
  internal/jobs/           Job Engine contracts
  internal/audit/          append-only audit service
  internal/secrets/        encryption + SecretStore
  internal/system/         host/WSL detection, component status and doctor
  internal/webui/          production static frontend serving/fallback
  internal/runtimes/       Runtime detection and runtime contracts
  internal/containerspec/  managed application image specifications
  internal/providers/      cross-module provider contracts
  internal/applications/   application/source/workload/endpoint lifecycle, API and reconciliation
  internal/drivers/        managed, Dockerfile, OCI image and Compose deployment drivers
  internal/projects/      legacy Project module; not mounted by the active composition root
  internal/docker/         owned by Docker agent
  internal/databases/      owned by Database agent
  internal/proxy/          owned by Reverse Proxy agent
frontend/                   React/Vite UI shell and domain UI modules
migrations/                 ordered, immutable SQL migrations
docs/adr/                   architecture decisions
scripts/                    developer/operator entrypoints
```

## Layering

HTTP handlers parse and validate transport input, call services and convert domain failures to the standard API error envelope. Services own orchestration and authorization-sensitive behavior. Repositories own persistence only. Providers own interaction with external tools or operating-system facilities.

Concrete provider implementations may depend on shared contracts and infrastructure utilities. They must not depend on another provider implementation. For example, Docker may use `PortAllocator` but must not import the concrete MySQL package.

## API modules

`api.Module` is the HTTP extension boundary. A domain can expose a module with its own route registration without adding handlers to the core API type. Core authentication middleware and role middleware are supplied through `api.ModuleMiddleware`.

A future integration agent may wire modules in the composition root. Domain agents should keep their implementation self-contained so wiring is a small, reviewable change.

Core host introspection remains under `/api/v1/system/*`. Component/platform endpoints are read-only and require an authenticated Viewer or higher role.

## Authentication and RBAC

Authentication uses random opaque session tokens. Only a SHA-256 token hash is stored in SQLite. The session token is sent as an HttpOnly, SameSite=Lax cookie. TLS deployments should set `DEVBOX_COOKIE_SECURE=true`.

Roles are ordered by capability: Admin > Operator > Viewer. Endpoints select the minimum required role explicitly. Authorization rules more granular than these roles should be introduced as a separate policy layer rather than hard-coded across providers.

## Database and migrations

SQLite is the bootstrap database. Foreign keys, WAL and busy timeout are enabled at startup. Migration files under `migrations/` are applied lexicographically and recorded in `schema_migrations`. Applied migration files are immutable.

## Secrets

`secrets.SecretStore` defines persistence access. `secrets.AESGCM` provides authenticated encryption using a 32-byte master key supplied out-of-band as base64. The current foundation does not invent or persist a default master key.

Provider configuration should store references to secrets, never raw secret values.

## Jobs

Potentially slow or external mutations should execute through `jobs.JobRunner`. HTTP handlers should enqueue work and return a job identity instead of invoking long-running processes directly. Handlers are registered by job type. Implementations must support durable state and logs through the `jobs` and `job_logs` tables.

Privileged component installation is not exposed synchronously through the System Components HTTP API. Privileged mutations must use audited jobs and the typed privileged-helper boundary.

## Runtime and application execution model

Docker is the mandatory execution boundary for managed applications. DevBox no longer deploys PHP, Go, Node.js, Python or static applications as host processes.

The runtime registry is retained for source-based runtime and framework detection. Project deployment does not call host lifecycle methods such as InstallDependencies, Build or Start for supported application runtimes.

Container resolution is deterministic:

1. a project-owned Compose file is used when present;
2. otherwise a project-owned Dockerfile is used when present;
3. otherwise, with container policy `auto`, DevBox generates an allowlisted managed image specification for PHP, Node.js, Python, Go or static content;
4. container policy `custom` requires a project-owned Compose file or Dockerfile.

Per-project runtime version and module selections are stored in SQLite. Module names are validated against runtime-specific catalogs; arbitrary package names or shell fragments are not accepted through this API.

Managed build contexts are staged outside the project tree and exclude `.env*`, VCS metadata, dependency directories and common local caches. Secrets are not written to generated Dockerfiles or image layers.

A content fingerprint covers the runtime, selected version/modules, source revision and sanitized build context. The Job Engine rebuilds only when the fingerprint changes or an operator requests a forced rebuild. Managed containers are replaced atomically: the previous container is retained until the new container starts and passes its health check, then removed; failures restore the previous container.

Host ports continue to come from the central PortAllocator and successful deployments are attached to the existing reverse-proxy routing layer.

## Project database connectivity

Credential-bearing database connectivity is resolved per project through the existing persisted database binding with modes `none`, `managed`, `compose` and `external`. In addition, `project_database_service_access` records whether a project exposes the shared DevBox MySQL/MariaDB service, PostgreSQL service or both. That service selection stores no database name, SQL account, password or grants; those remain centrally managed by the Databases module. Previously persisted credential-free `host_access_only` bindings remain readable for compatibility but are no longer created by the project UI.

The project database UI treats the shared DevBox services as infrastructure endpoints with non-editable application addresses. MySQL/MariaDB is reached through Docker DNS `devbox-mysql:3306` and PostgreSQL through `devbox-postgresql:5432`, both on `devbox-apps`. Selecting one or both services does not provision a database or user and does not duplicate credentials. Existing credential-bearing Compose/external bindings keep their current connection forms and SecretStore behavior.

Application database servers installed from Plugins are persistent Docker services on the shared `devbox-apps` network. MySQL uses `devbox-mysql` with `devbox-mysql-data`; PostgreSQL uses `devbox-postgresql` with `devbox-postgresql-data`. Their administrative credentials are SecretStore-backed. Every DevBox application container—including generated runtimes, custom Dockerfile deployments and running services from project-owned Compose—is attached to the same shared network, so application code can use stable Docker DNS names (`devbox-mysql:3306`, `devbox-postgresql:5432`) without exposing database ports publicly. Managed/container deployments pass sensitive runtime variables through protected temporary env files; project-owned Compose receives private overrides outside the source tree. Project Compose files and generated Dockerfiles never receive stored secret plaintext.

Runtime environment precedence is deterministic: generated runtime defaults < explicit project environment < project SecretStore environment < reserved database binding variables. Compose application-service selection prefers explicit configuration, then a DevBox label, then deterministic heuristics; ambiguity requires an explicit UI choice.

Connection tests execute `SELECT 1`. Managed and external tests run from the Docker execution boundary; Compose tests execute against the selected database service. No WSL host/subnet IP is persisted. See ADR-011.

## Provider contracts

Stable contracts live in `backend/internal/providers/contracts.go`:

- `GitProvider`
- `ProcessManager`
- `DockerProvider`
- `DatabaseProvider`
- `ReverseProxyProvider`
- `PortAllocator`
- `SystemServiceProvider`
- alias `SecretStore`
- alias `JobRunner`

The types next to those interfaces are transport-neutral orchestration DTOs. Provider-specific settings belong inside the provider module rather than expanding shared types for every implementation detail.

## Windows / WSL and installation

Windows uses WSL as the Linux execution boundary. `install.ps1` detects WSL and supported Ubuntu/Debian distributions, can bootstrap Ubuntu explicitly, verifies/enables WSL systemd when required and delegates installation to `install.sh` inside the selected distribution.

The Linux installer builds backend and frontend, installs the control plane under `/opt/devbox` and `/usr/local/lib/devbox`, stores mutable data under `/var/lib/devbox` and installs `devbox.service` under the unprivileged `devbox` account. `DEVBOX_FRONTEND_DIR` lets the Go process serve the built SPA and API on one listener.

## Privilege boundary

The HTTP API runs unprivileged. Operations requiring Administrator/root privileges must be delegated through a narrow, auditable system-service boundary rather than running the entire API as Administrator/root. See ADR-006 and ADR-008.

`devbox-helper` exposes only typed, whitelisted operations. It cannot execute an arbitrary executable/argument vector, cannot write an arbitrary path and cannot install a package name supplied directly by a caller without allowlist mapping.

### Project port publishing

`projects.PortConfiguration` keeps desired settings and the last applied mapping in additive SQLite state. Deployment jobs use the central `PortManager` through the optional `SequentialPortAllocator`/`PortLeaseOwner` contracts, keep previous leases until a replacement is healthy, and retain leases when cleanup cannot safely confirm the ports are unused. Resolved host ports become the stable project settings. Generated container listeners are configured in `containerspec`; actual Docker and opt-in Compose publication remains in the Docker provider. Compose overrides are stored in a private durable directory, not in the application source tree. See ADR-010 and `docs/project-port-publishing.md`.

## Application ownership

ADR 012 defines the active Application control plane. The workspace is no longer a thin UI over Project records. Jobs, desired/observed state, staged topology, stable endpoint leases and encrypted application secrets have dedicated contracts. See `docs/application-control-plane.md` for API, concurrency, deletion and compatibility boundaries.

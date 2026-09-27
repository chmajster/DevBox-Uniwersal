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
  internal/runtimes/       Runtime contract
  internal/providers/      cross-module provider contracts
  internal/projects/       owned by Git/Projects agent
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

## Runtime model

`runtimes.Runtime` is intentionally lifecycle-oriented: Detect, Validate, InstallDependencies, Build, Start, Stop, Restart, Status, Logs and HealthCheck. Runtime implementations receive a `ProjectContext`; they do not own Project persistence.

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

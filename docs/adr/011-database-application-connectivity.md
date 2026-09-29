# ADR-011: Project database application connectivity

Status: Accepted.

## Context

DevBox executes applications inside Docker. A control-plane MySQL endpoint such as `127.0.0.1:3306` is therefore not a valid application endpoint: inside an application container loopback points back to that container. Project-owned Compose has a different rule again, because services in the same Compose project address one another by service DNS, for example `db:3306`.

The database subsystem already owns provisioning, users, grants, backups and SecretStore-backed credentials. The Docker subsystem already owns managed containers and private Compose overrides. The solution must reuse those capabilities rather than create parallel provisioning paths.

## Decision

### Database binding

Each project has at most one `project_database_bindings` row with one of four transport-neutral modes:

- `none`: no database connection is injected;
- `managed`: a database/user managed by the existing DevBox database subsystem;
- `compose`: a database service owned by the project's Compose definition;
- `external`: an externally operated MySQL/MariaDB endpoint.

Managed bindings reference the existing `databases` row. Database identity remains authoritative in `databases`; managed SQL accounts live in `database_accounts`, while per-database assignments and privileges live in `database_user_grants`. A single account may therefore be granted access to multiple databases on the same SQL server. The project binding does not duplicate this state. Compose/external bindings persist connection metadata and an opaque SecretRef only.

The released schema still contains the `host_access_only` compatibility flag from migration `010_project_database_host_access.sql`. Existing bindings with that flag remain readable and preserve their host-gateway behavior, but the project UI no longer creates them and DevBox no longer auto-discovers host MySQL/MariaDB/PostgreSQL services for project configuration. New host SQL connections should be represented as ordinary external bindings with explicit connection credentials when that topology is intentionally required.

Migration `009_project_database_bindings.sql` is additive and backfills existing per-project databases as `managed` bindings without rotating users or passwords. Migration `010_project_database_host_access.sql` remains part of the schema for backwards compatibility.

### Shared DevBox database service selection

A project can independently record access to the shared DevBox SQL services in `project_database_service_access`. The allowed choices are MySQL/MariaDB, PostgreSQL, or both. This record is intentionally credential-free: it contains only boolean service selections and a timestamp. It does not contain a database name, SQL account, password, SecretRef, or grant set.

The project UI presents those services as non-editable infrastructure endpoints. MySQL/MariaDB resolves to `devbox-mysql:3306`; PostgreSQL resolves to `devbox-postgresql:5432`; both are on the shared `devbox-apps` network. Container names may still be supplied by the managed-server configuration, but users do not edit connection hosts or ports in the project form.

Database creation, account lifecycle, password rotation and per-database privileges remain authoritative in the Databases module. Selecting a shared service does not provision a database or account and does not inject an arbitrary credential. Existing credential-bearing bindings remain supported for Compose/external compatibility, and previously persisted managed bindings remain readable until the user saves the new shared-service selection.

Migration `012_project_database_service_access.sql` adds this state without modifying released binding migrations.

### Admin and application endpoints

The MySQL provider exposes separate endpoint semantics:

- control-plane administration: loopback TCP, normally `127.0.0.1:<admin-port>`;
- application traffic: Docker DNS `devbox-mysql:3306`.

Provisioning, users, grants, backup/restore and health administration use the admin endpoint. Runtime injection uses the application endpoint. Code must not infer application connectivity from the admin endpoint.

SQL account lifecycle is server-scoped, while grants are database-scoped. Password changes affect the account across all assigned databases. Removing one database assignment revokes only that database's privileges; the SQL account is dropped only when the account itself is deleted or when deletion of its final managed database leaves it with no assignments.

### Managed database server lifecycle

Database servers installed from Plugins are Docker-native:

- MySQL container: `devbox-mysql`, image `mysql:8.4` by default, persistent volume `devbox-mysql-data`, application endpoint `devbox-mysql:3306`;
- PostgreSQL container: `devbox-postgresql`, image `postgres:17` by default, persistent volume `devbox-postgresql-data`, application endpoint `devbox-postgresql:5432`;
- shared external Docker network: `devbox-apps` (configurable with `DEVBOX_APP_NETWORK`);
- restart policy: `unless-stopped`;
- MySQL control-plane administration remains loopback-only on a dedicated configurable host port (default `13306`);
- PostgreSQL has no host port publication by default.

Network, volume, image and container reconciliation are idempotent. Database administrative credentials are stored in SecretStore and supplied to Docker through protected temporary environment files rather than command arguments or image layers. Standard restart never removes persistent database volumes.

Plugin install actions run through the durable Job Engine and perform Docker image/network/volume/container reconciliation. They do not install MySQL/MariaDB or PostgreSQL server packages on the host. The project UI treats plugin databases as Docker services, not as host SQL packages.

### Runtime environment and precedence

Deployment resolves runtime environment before starting the application. The deterministic precedence from lowest to highest is:

1. generated/runtime image defaults;
2. explicit project runtime environment;
3. project secret environment resolved from SecretStore;
4. reserved database binding variables.

Reserved database variables are:

`DB_DRIVER`, `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD`,
plus `DATABASE_HOST`, `DATABASE_PORT`, `DATABASE_NAME`, `DATABASE_USER`, `DATABASE_PASSWORD`.

This prevents a stale user value such as `DB_HOST=127.0.0.1` from overriding the selected database binding.

For managed/Dockerfile containers, sensitive values are supplied through a temporary mode-0600 env file which is removed after container creation. They are never written to the generated Dockerfile or project `.env`.

### Project-owned Compose

The user's Compose file remains authoritative and is never rewritten. DevBox stores a separate mode-0600 database/runtime override outside the project source tree. Application service selection uses:

1. explicit project binding;
2. exactly one service labelled `io.devbox.application=true`;
3. deterministic application-service heuristics;
4. otherwise an explicit UI selection is required.

For Compose database mode the application receives the selected service DNS, e.g. `DB_HOST=db`. Independently of binding mode, every running service in a DevBox project-owned Compose deployment is attached to the external `devbox-apps` network after `compose up`. Managed and custom-Dockerfile application containers join the same network before start. Plugin database servers are therefore reachable from DevBox application containers by stable Docker DNS.

Existing Docker Compose v2 / legacy `docker-compose` fallback remains intact. Commands use compatible `-p` / `-f` options and do not use the previously incompatible `--project-directory` or `--project-name` invocation.

### Connectivity tests

A connection test is real, not a status flag:

- Compose mode executes `SELECT 1` against the selected database service;
- managed and external modes execute `SELECT 1` from a short-lived Docker MySQL client, using the same Docker network class as the application;
- managed mode therefore validates `devbox-apps -> devbox-mysql:3306`.

Passwords are passed through process environment or protected temporary files, never command arguments or logs.

### PHP

A generated PHP runtime with an active database binding must include either `pdo_mysql` or `mysqli`. The project UI reports the missing driver and can add `pdo_mysql` to the existing per-project runtime module configuration. No host PHP extension is installed.

### WSL

No WSL host IP or Docker subnet is persisted. Application-to-managed-MySQL communication uses Docker DNS and the shared network, so WSL address changes do not change project bindings.

Managed database traffic does not depend on a WSL or host address: applications use Docker DNS on `devbox-apps`. For explicitly configured external MySQL/MariaDB bindings, loopback input such as `127.0.0.1`, `localhost` or `::1` is still normalized to `host.docker.internal` and DevBox adds `host.docker.internal:host-gateway` where Linux/WSL Docker requires it. This is an external-database compatibility path, not the normal managed-database topology.

## Consequences

- Managed application containers, project-owned Dockerfiles and project-owned Compose use the same binding resolver.
- Existing backup/restore and database-user/grant implementations remain authoritative.
- phpMyAdmin joins `devbox-apps` and targets the configured MySQL endpoint directly. In the standard managed setup that endpoint is `devbox-mysql:3306`; host-gateway mapping is added only for an explicitly configured legacy loopback target. Existing phpMyAdmin containers are reconciled on install/start/restart when this networking configuration changes.
- The API never returns stored password plaintext from binding reads or password rotation.
- Deleting a managed project database removes its binding so stale application endpoints are not retained.

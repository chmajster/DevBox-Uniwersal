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

Managed bindings reference the existing `databases` row. Database name, managed username and managed user SecretRef remain authoritative in `databases` / `database_users`; the binding does not duplicate them. Compose/external bindings persist connection metadata and an opaque SecretRef only.

An external binding may additionally set `host_access_only`. In that variant the binding is not a database credential configuration: DevBox stores only the Docker-host target plus the selected engine/port, injects the host-gateway mapping at deployment time, does not persist a database name/user/password, does not resolve SecretStore credentials and does not inject `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD` or their `DATABASE_*` aliases. Host-access-only bindings may target MySQL, MariaDB or PostgreSQL. Application code remains responsible for its own database/schema credentials.

Migration `009_project_database_bindings.sql` is additive and backfills existing per-project databases as `managed` bindings without rotating users or passwords. Migration `010_project_database_host_access.sql` adds the access-only flag without changing the released mode constraint.

### Admin and application endpoints

The MySQL provider exposes separate endpoint semantics:

- control-plane administration: loopback TCP, normally `127.0.0.1:<admin-port>`;
- application traffic: Docker DNS `devbox-mysql:3306`.

Provisioning, users, grants, backup/restore and health administration use the admin endpoint. Runtime injection uses the application endpoint. Code must not infer application connectivity from the admin endpoint.

### Managed database server lifecycle

Database servers installed from Plugins are Docker-native:

- MySQL container: `devbox-mysql`, image `mysql:8.4` by default, persistent volume `devbox-mysql-data`, application endpoint `devbox-mysql:3306`;
- PostgreSQL container: `devbox-postgresql`, image `postgres:17` by default, persistent volume `devbox-postgresql-data`, application endpoint `devbox-postgresql:5432`;
- shared external Docker network: `devbox-apps` (configurable with `DEVBOX_APP_NETWORK`);
- restart policy: `unless-stopped`;
- MySQL control-plane administration remains loopback-only on a dedicated configurable host port (default `13306`);
- PostgreSQL has no host port publication by default.

Network, volume, image and container reconciliation are idempotent. Database administrative credentials are stored in SecretStore and supplied to Docker through protected temporary environment files rather than command arguments or image layers. Standard restart never removes persistent database volumes.

Plugin install actions run through the durable Job Engine and perform Docker image/network/volume/container reconciliation. They do not install MySQL/MariaDB or PostgreSQL server packages on the host. Legacy host databases remain supported as external/host-access targets.

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

For a database server running on the Docker host, DevBox uses the stable application hostname `host.docker.internal`. User input of `127.0.0.1`, `localhost` or `::1` is normalized for credentialed MySQL/MariaDB traffic, while managed/custom containers and generated Compose overrides receive the explicit Docker mapping `host.docker.internal:host-gateway`. The dedicated host-access-only UI detects installed MySQL/MariaDB and PostgreSQL services, preserves the chosen port (PostgreSQL clusters are read from `pg_lsclusters` when available), and deliberately does not collect or inject database credentials. This avoids persisting a bridge or WSL IP. The host database still has to listen on an interface reachable from Docker and, when the application authenticates to it, the database account/grants or PostgreSQL `pg_hba.conf` must allow the container-side connection.

## Consequences

- Managed application containers, project-owned Dockerfiles and project-owned Compose use the same binding resolver.
- Existing backup/restore and database-user/grant implementations remain authoritative.
- phpMyAdmin joins `devbox-apps` for managed MySQL and also receives `host.docker.internal:host-gateway`. Its container exposes both the managed `devbox-mysql` target and host MySQL/MariaDB on `host.docker.internal:3306`, with arbitrary-server login enabled. Existing phpMyAdmin containers are reconciled on install/start/restart when this networking configuration is missing.
- The API never returns stored password plaintext from binding reads or password rotation.
- Deleting a managed project database removes its binding so stale application endpoints are not retained.

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

Migration `009_project_database_bindings.sql` is additive and backfills existing per-project databases as `managed` bindings without rotating users or passwords.

### Admin and application endpoints

The MySQL provider exposes separate endpoint semantics:

- control-plane administration: loopback TCP, normally `127.0.0.1:<admin-port>`;
- application traffic: Docker DNS `devbox-mysql:3306`.

Provisioning, users, grants, backup/restore and health administration use the admin endpoint. Runtime injection uses the application endpoint. Code must not infer application connectivity from the admin endpoint.

### Managed MySQL lifecycle

New managed installations use:

- container: `devbox-mysql`;
- image: `mysql:8.4` by default;
- external Docker network: `devbox-apps`;
- persistent volume: `devbox-mysql-data`;
- restart policy: `unless-stopped`;
- admin publication bound to `127.0.0.1` only.

Network, volume, image and container reconciliation are idempotent. Standard restart never removes the persistent volume. The managed root credential is stored in SecretStore and is supplied to Docker through a mode-0600 temporary environment file rather than a command argument or image layer.

Managed lifecycle mutations use the durable Job Engine. A clean installer uses managed MySQL and installs only a MySQL client on the host. Existing installations that predate this ADR and contain the old host-MySQL admin credential remain in legacy host mode on repair/update unless `DEVBOX_MYSQL_MANAGED=true` is explicitly selected; DevBox does not silently stop an existing host database server.

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

For Compose database mode the application receives the selected service DNS, e.g. `DB_HOST=db`. For managed mode the selected application service is additionally attached to the external `devbox-apps` network and receives `DB_HOST=devbox-mysql`.

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

For an external MySQL/MariaDB server running on the Docker host, DevBox uses the stable application hostname `host.docker.internal`. User input of `127.0.0.1`, `localhost` or `::1` is normalized for application traffic, while managed/custom containers and generated Compose overrides receive the explicit Docker mapping `host.docker.internal:host-gateway`. This avoids persisting a bridge or WSL IP. The host database still has to listen on an interface reachable from Docker and its database account/grants must allow the container-side connection.

## Consequences

- Managed application containers, project-owned Dockerfiles and project-owned Compose use the same binding resolver.
- Existing backup/restore and database-user/grant implementations remain authoritative.
- phpMyAdmin joins `devbox-apps` and targets `devbox-mysql` in managed mode.
- The API never returns stored password plaintext from binding reads or password rotation.
- Deleting a managed project database removes its binding so stale application endpoints are not retained.

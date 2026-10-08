# Application hosting

DevBox has one application model and one `/apps` workspace. Every application runs in Docker. The five wizard steps are Source, Technology, Startup, Database and Summary; the final actions save the configuration or save and deploy it. Local directories have a tree browser, search and manual path input. Git supports a reference and a saved credential; empty sources receive a working starter; OCI sources run an existing image without inventing a source mount.

## Execution modes

| Mode | Source authority | Execution |
| --- | --- | --- |
| Existing Compose (`compose`) | Root `compose.yaml`, `compose.yml`, `docker-compose.yml` or `docker-compose.yaml` | Retains services, dependencies, networks, healthchecks and volumes. Select an HTTP service when several are possible. Private overrides supply DevBox configuration without rewriting the source. |
| Existing Dockerfile (`dockerfile`) | Source Dockerfile | Builds the source image, reads the final WORKDIR/EXPOSE and mounts code RW at the selected compatible target. Ambiguous ports and missing WORKDIR require configuration. |
| Generated Docker (`auto`) | Selected runtime/version | Generates PHP, Python, Go, Node.js or static HTTP hosting outside the source. WordPress is a PHP/Apache profile. |

OCI is a source-only exception implemented by the same managed container engine. Internally there are two deployment engines: Compose and managed; Dockerfile and OCI do not create parallel application lifecycle systems. Selecting Generated explicitly bypasses detected Dockerfile/Compose. Settings can change the mode. A changed runtime/version queues Rebuild for a provisioned application.

The backend runtime catalog (`GET /api/v1/runtimes/catalog`) supplies version lists and official image templates to the UI. A custom exact version is also possible. DevBox verifies the selected upstream image before deployment; a nonexistent tag fails the job without silently substituting another version.

## Requirements and sources

The control plane requires SQLite, Docker Engine and Docker Compose 2.24.4+. Nginx is required for domains. The DevBox service account must have daemon access and source traversal/read/write permissions. Go and Node installed by the source installer build DevBox; application interpreters, compilers and dependencies run inside images.

`DEVBOX_PROJECTS_ROOT` contains managed Git/empty sources. `DEVBOX_DIRECTORY_BROWSE_ROOTS` adds local roots, separated by `:` on Linux/WSL, for example `/home/chris/apps:/mnt/c/Projects`. Select an existing absolute application directory. Browser and deployment use the same authorization. Protected system/credential directories, traversal and symlink escapes are rejected.

For WSL use the Linux path `/mnt/c/...`, not a Windows drive string. The actual mount must exist in the same daemon environment as DevBox. The integration suite can exercise an explicit Windows-backed root with `DEVBOX_TEST_WSL_ROOT`; it verifies edits and container-created files in both directions. Windows filesystem permission semantics differ from native Linux UID/GID ownership. Docker Desktop must expose the selected WSL distribution if that daemon is used.

## Runtimes and writable code

| Source | Selection | Container port | Default/example command |
| --- | --- | --- | --- |
| `index.php`, optional `composer.json` | PHP 8.4 | 8080 | `apache2-foreground`; document root `.` or `public` |
| `requirements.txt`, FastAPI `main.py` | Python 3.13 | 8000 | `python -m uvicorn main:app --host 0.0.0.0 --port 8000` |
| `go.mod`, `main.go` or one `cmd/*/main.go` | Go 1.26 | 8080 | Generated startup downloads modules, builds `/tmp/devbox-app` and executes the binary |
| `package.json`, `server.js` | Node.js 22 | 3000 | `npm start`; npm/pnpm/yarn selected from package metadata |
| `index.html` | Static/Nginx 1.28 | 8080 | `nginx -g 'daemon off;'` |
| WordPress directory | PHP 8.4 / WordPress | 80 | `apache2-foreground` |

All web commands must stay in the foreground and listen on `0.0.0.0` at the configured container port. Python detection suggests FastAPI, Flask, Django or an existing HTTP entrypoint; requirements and pyproject dependencies are installed in the image. PHP supports Composer and per-application extensions including pdo_mysql, pdo_pgsql, mysqli, gd, zip, intl, mbstring and opcache; development opcode caching checks source changes. Go supports manual Rebuild after editing and recompiles on container startup. Automatic file watching and a separate production Go image are not implemented.

Generated PHP/Node/Python/Go mount the original directory at `/app:rw`; WordPress uses `/var/www/html:rw`, static uses `/usr/share/nginx/html:rw`. Dockerfile mode uses the final WORKDIR or explicit mount target. Compose retains existing mounts, makes source-tree binds RW and supplies a compatible source bind to application workloads with a working directory. PHP vendor and Node node_modules use dependency volumes so live source does not hide installed packages.

Non-root generated processes use the writable source owner's UID/GID. Apache runs its parent with restricted capabilities and uses a named worker account mapped to the source owner. DevBox never applies global chmod 777 or recursively changes the user's source ownership. Compose retains its declared process user. Root-owned or inaccessible Linux sources require operator permission correction. WordPress config initialization writes environment references, not a plaintext password; uploads remain in the original directory.

Runtime artifacts live at `<SQLite directory>/projects/<application-id>/runtime/`: `Dockerfile`, `compose.yaml`, `metadata.json`. They exclude secret values and can be exported through the application UI or `GET /api/v1/applications/{id}/export`. Generated Compose describes equivalent managed execution; lifecycle execution uses controlled Docker argv and replacement logic. CPU/RAM/network I/O comes from `GET /api/v1/applications/{id}/stats`.

## Shared SQL servers

MySQL, MariaDB and PostgreSQL are independent Docker servers with persistent named volumes. They start once and serve multiple applications. Application containers have private networks plus the configured shared application network (default `devbox-apps`). Compose web/API/worker/scheduler workloads join it through an override. Docker DNS supplies server names; application SQL does not require a published database host port. Installer defaults may publish an administrator port only on loopback.

The Database step supports no database, an existing database/account, a new account on an existing database, or a new database and account on a selected server. Provisioning prepares the server, creates the database/account/grants, generates an encrypted SecretStore password and binds the exact account. It rejects root/postgres as application accounts. The details tab can bind, detach and run a connection test. Bind/detach requires another Deploy to change a running container's environment.

DevBox injects `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_DATABASE`, `DB_USER`, `DB_USERNAME`, `DB_PASSWORD` and driver metadata into application workloads. WordPress also receives its conventional variables. Infra Compose services do not receive application SQL credentials. Networking provides reachability; SQL grants independently control data access. PostgreSQL revokes PUBLIC database access/schema creation and grants only the selected account's privileges.

The test job copies the installed static `devbox-dbcheck` probe into the actual chosen workload and executes authentication and `SELECT 1` with the bound account/database. Credentials travel over stdin and never appear in argv or probe errors. Install/update installs the sibling executable; source deployments can set `DEVBOX_DATABASE_PROBE` to a built probe. It supports MySQL, MariaDB and PostgreSQL without requiring client packages in application images.

Native API:

- `GET /api/v1/database-servers`; `POST /api/v1/database-servers/{engine}/{install|start|stop|restart}` queues non-destructive server operations.
- `GET/PUT/DELETE /api/v1/applications/{id}/database-binding`; PUT takes `database_id` and `user_id`.
- `POST /api/v1/applications/{id}/database-binding/provision` takes engine/name/username/privileges, or database_id for a new account on an existing database.
- `POST /api/v1/applications/{id}/database-binding/test` optionally takes workload name.

Rebuild/restart/delete of an application preserves shared SQL servers, volumes and data. Detach does not delete the database/account. Historical project SQL records remain readable for inventory/backups; legacy project SQL mutation routes are retired. Application database bindings are the active integration, with migration 020 preserving the selected account.

## Ports, readiness and lifecycle

The internal port is the process listener; the host port is its publication. Auto allocation starts at 8080, with durable SQLite leases and checks for host listeners and Docker publications. Restart and stop retain the port. Delete releases it. Conflicts identify the occupied port without stealing another application's lease. HTTP links appear only for a running, ready published endpoint.

Docker observation and HTTP readiness are separate. Reconciliation polls actual resources; unavailable Docker invalidates observation. A desired-running missing/exited workload or absent publication fails. Managed and Compose deployment wait for stable containers, declared Docker healthchecks and the selected HTTP endpoint. HTTP status below 500 establishes reachability, not business correctness; configure the health path and Compose healthchecks for stronger checks.

| Action | Behavior |
| --- | --- |
| Deploy / Redeploy | Analyze, plan, prepare image, launch, verify readiness and routing |
| Build | Build without replacing workloads |
| Start / Stop / Restart | Operate existing containers, preserve data/configuration/leases |
| Rebuild | Force image build and safe replacement/recreation |
| Recreate | Replace containers using configuration and the existing suitable image |
| Compose Pull / Down | Pull source images / remove stack while retaining data and leases |
| Delete | Remove owned app resources, routes, secrets, leases and generated files; preserve original sources, images and persistent named volumes |

After Compose Down use Deploy/Recreate before Start. Jobs expose stage/progress, streams, timestamps, errors, cancellation and retry. The application operation lock prevents conflicting mutations. Logs poll and support service/stream selection, search and tailing. Settings, database bindings, containers, mounts, health, CPU/RAM, operation history and addresses are visible in details.

Managed replacement retains the old container and restores it if startup fails. Compose command/port updates attempt to restore the previous successful plan, images, topology and leases without rebuilding. This recovery is best effort: source Compose changes, external resources and data writes are not transactional. Same-port replacement can briefly interrupt service.

Domains use the published loopback endpoint through Nginx. Candidate and global configurations are checked before reload. Domain ownership is unique. Existing TLS certificates are supported under `<SQLite directory>/certificates/<hostname>/`; key/certificate/hostname/validity are checked. Client DNS is configured separately. Let's Encrypt stays disabled until an ACME issuer/challenge workflow is implemented.

## Secrets and resource safety

RBAC is Admin/Operator/Viewer; mutations require Operator and same-origin validation. Public environment is stored separately from encrypted SecretStore entries. The KEY/VALUE editor masks secrets and imports .env without shell evaluation. Resolved SQL credentials override application/public variables. Generated files and snapshots omit secret values; temporary environment files are private and logs redact known credentials. Source-provided Compose interpolation/env_file still follows Compose semantics. Host/Docker administrators can inspect running container environment.

Applications cannot request privileged containers, host networking/PID/IPC, device access, unsafe added capabilities, Docker socket binds, source paths outside their authorized directory, privileged/SSH build access, or host-bind volume driver options. Dockerfile/build contexts and Compose config/secret files are checked for escapes. Named external volumes/networks are source-authorized shared resources and retain their external lifecycle.

Resource names include immutable application IDs. Ownership labels are checked before replacement/removal; DevBox refuses to replace foreign infrastructure containers. App deletion preserves persistent volumes and never deletes a shared server. Deliberate volume deletion remains a separate operator action.

## Installation and verification

See [INSTALLER.md](INSTALLER.md). `--reinstall` / Windows `-Mode Reinstall` replaces software while preserving SQLite, keys, configuration, sources and database volumes. Purge is separate and only belongs to explicit uninstall. Doctor checks Docker Engine, Compose, daemon access and shared networking; application runtimes do not require host language packages.

```bash
cd backend
go test ./...
go vet ./...
go test -race ./internal/applications ./internal/drivers/... ./internal/docker ./internal/databases ./internal/proxy ./internal/jobs
go build ./cmd/devbox ./cmd/devbox-helper ./cmd/devbox-dbcheck
cd ../frontend
npm test
npm run lint
npm run typecheck
npm run build
cd ..
bash scripts/test-install.sh
python3 -m unittest discover -s frontend/e2e -p 'test_*.py'
```

Real Docker integration requires a disposable Docker environment with image network access and enough space:

```bash
mkdir -p .build
cd backend
CGO_ENABLED=0 go build -o ../.build/devbox-dbcheck ./cmd/devbox-dbcheck
DEVBOX_TEST_APPLICATION_DOCKER=1 go test ./internal/applications -run '^TestApplicationDocker' -v -count=1 -timeout=50m
# WSL: additionally set DEVBOX_TEST_WSL_ROOT=/mnt/c/.../disposable-test-root
```

The Docker CI workflow runs the probe build, race regressions and the real suite. Normal unit tests skip opt-in containers. Do not infer actual SQL/mount readiness from mocked tests.

| CODEX_TASK scenario | Real integration assertions |
| --- | --- |
| A PHP | Local source, selected PHP version, Apache HTTP, edited PHP served immediately, container file visible on host |
| B Python | FastAPI dependencies, actual Python version, HTTP and RW writes |
| C Go | go.mod, actual Go version, build/binary/port, visible sources and changed response after Rebuild |
| D Node | Installed dependency is required by the app, actual Node version, HTTP and RW writes |
| E SQL | MySQL/PostgreSQL/MariaDB, PHP+FastAPI accounts, authenticated SELECT 1 inside app containers, cross-database refusal and persistent row after rebuild/server restart |
| F Compose | web+worker+Redis+private database, correct HTTP selection, shared-network membership, actual SQL from web and worker, preserved source/config/data |
| G failures | Busy port, missing OCI image/runtime tag, failed build/command, unavailable SQL, denied source, control-plane reconstruction, failed command/port update and recovery |

Additional real scenarios cover static HTML, WordPress uploads/config, concurrent PHP versions, both Compose filenames, Dockerfile WORKDIR, OCI without mount, Git and empty sources, stable lifecycle ports, stop/start/restart/rebuild/recreate, source-preserving deletion, and optional Windows-backed WSL sources. [STATUS.md](../STATUS.md) records executed results and verification limits.

## Troubleshooting

- Docker permission/unavailable: run `docker info` as the DevBox service account, enable the selected daemon/WSL integration and correct socket group access.
- Rejected directory: add its parent to browse roots and ensure service traversal permissions; do not select a protected system/credentials root.
- Container failed: inspect the job stage and workload logs; verify executable, dependencies, bind address, listener port and health path.
- Running container without HTTP readiness: verify the primary Compose workload, actual listener and publication; do not substitute an infrastructure port.
- SQL test failed: check selected account/grants, server health, shared network and installed probe; test from a running app workload.
- Source write failed: inspect owner UID/GID and permissions. Compose user remains source-owned; avoid global chmod/chown.
- Manifest changed: use Rebuild to refresh dependency images/volumes. PHP/static edits are live; compiled Go code needs rebuilding/restarting.
- Domain failed: inspect DNS/certificates and validated Nginx diagnostics; direct HTTP remains available if the container is healthy.

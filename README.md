# DevBox Universal

DevBox Universal is a local control plane for installing, running and managing heterogeneous development applications from a web UI. The foundation intentionally separates orchestration contracts from concrete Git, runtime, Docker, database, reverse-proxy and Windows/WSL implementations.

## Application workspace

The `/apps` workspace runs applications from a local directory, Git repository, editable starter or OCI image. A five-step wizard selects **existing Compose**, **existing Dockerfile** or **generated Docker**, runtime/version, startup settings and an optional SQL database. Generated PHP uses Apache; Python, Go, Node.js and static sites have dedicated container profiles. Source applications use live RW mounts and retain their dependencies. OCI images run without a source mount.

MySQL, MariaDB and PostgreSQL run as independent shared servers with persistent volumes. Applications use Docker DNS on `devbox-apps`, separate SQL accounts and encrypted credentials. The panel supports lifecycle jobs, HTTP readiness, logs, CPU/RAM, configuration export and per-application SQL tests that authenticate and execute `SELECT 1` inside the actual workload. See [Application hosting](docs/application-control-plane.md) for setup, examples and real Docker tests.

This refactor preserves existing Project records but does not automatically adopt them or migrate project-specific database/domain bindings. New application IDs are independent of legacy Project IDs.

## Foundation stack

- Backend: Go REST API, SQLite, ordered SQL migrations.
- Authentication: opaque server-side sessions, Admin/Operator/Viewer RBAC.
- Secrets: AES-256-GCM abstraction; plaintext secrets are never persisted.
- Frontend: React, TypeScript, Vite.
- Extension model: runtime/provider interfaces and per-domain API modules.
- Docker: local Docker Engine inventory/lifecycle plus controlled Docker Compose integration.

## Installers

Linux / WSL — one-line install:

```bash
curl -fsSL https://raw.githubusercontent.com/chmajster/DevBox-Uniwersal/main/install.sh | sudo bash -s -- --install
```

- Windows / WSL: [install.ps1](https://github.com/chmajster/DevBox-Uniwersal/blob/main/install.ps1) — [direct download](https://raw.githubusercontent.com/chmajster/DevBox-Uniwersal/main/install.ps1)
- Linux / WSL: [install.sh](https://github.com/chmajster/DevBox-Uniwersal/blob/main/install.sh) — [direct download](https://raw.githubusercontent.com/chmajster/DevBox-Uniwersal/main/install.sh)

The piped installer installs required packages, downloads the current `main` source tree, builds DevBox and installs the system service.

## Quick start

Requirements: Go 1.23+, Node.js 22+, npm. Docker is optional; the backend remains available when Docker is missing.

```bash
export DEVBOX_AUTH_DISABLED=true
export DEVBOX_BOOTSTRAP_ADMIN_USERNAME=admin
export DEVBOX_MASTER_KEY="$(openssl rand -base64 32)"
export DEVBOX_PROJECTS_ROOT="./projects"
./scripts/dev.sh
```

Backend: `http://127.0.0.1:8787`  
Frontend: `http://127.0.0.1:5173`

By default DevBox is local-only and passwordless: the UI opens directly as the local admin while the HTTP listener remains bound to loopback. Set DEVBOX_AUTH_DISABLED=false and configure bootstrap credentials before exposing DevBox beyond localhost.

## API

All stable endpoints are versioned under `/api/v1`.

Core:
- `GET /api/v1/health`
- `GET /api/v1/system/info`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/me`
- `GET /api/v1/jobs`
- `GET /api/v1/jobs/{id}`
- `GET /api/v1/audit`

Docker:
- `GET /api/v1/docker/status`
- `GET /api/v1/docker/containers`
- `POST /api/v1/docker/containers/{id}/start`
- `POST /api/v1/docker/containers/{id}/stop`
- `POST /api/v1/docker/containers/{id}/restart`
- `GET /api/v1/docker/containers/{id}/logs`
- `GET /api/v1/docker/images`
- `GET /api/v1/docker/volumes`
- `GET /api/v1/docker/networks`
- `GET /api/v1/docker/compose/projects`

Success envelope: `{ "data": ..., "meta": ... }`. Error envelope: `{ "error": { "code": "...", "message": "...", "details": ... } }`.

Authenticated API discovery:
- `GET /api/v1/openapi.json` — OpenAPI 3.1 contract.
- `GET /api/v1/docs` — human-readable endpoint index.

Project health checks run automatically for configured HTTP/TCP targets and retain history. Central logs support `source=all`, SSE live tail, time-range filters and text export.

Administrators can create, download, import and restore control-plane backups from the Backup DevBox page. Backup archives contain a consistent SQLite snapshot, encrypted secrets as stored in SQLite, managed Nginx state and controlled Docker/Compose manifests. `DEVBOX_MASTER_KEY` is never placed in the archive and must be retained separately.

See `ARCHITECTURE.md`, `AGENTS.md` and `docs/adr/` before adding a module.


## License

DevBox Universal is distributed under the MIT License. See `LICENSE`.

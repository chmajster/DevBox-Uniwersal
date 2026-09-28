# DevBox Universal

DevBox Universal is a local control plane for installing, running and managing heterogeneous development applications from a web UI. The foundation intentionally separates orchestration contracts from concrete Git, runtime, Docker, database, reverse-proxy and Windows/WSL implementations.

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
export DEVBOX_BOOTSTRAP_ADMIN_USERNAME=admin
export DEVBOX_BOOTSTRAP_ADMIN_PASSWORD='replace-with-a-long-password'
export DEVBOX_MASTER_KEY="$(openssl rand -base64 32)"
export DEVBOX_PROJECTS_ROOT="./projects"
./scripts/dev.sh
```

Backend: `http://127.0.0.1:8787`  
Frontend: `http://127.0.0.1:5173`

The bootstrap admin is created only when the users table is empty. Do not store production credentials in shell history or repository files.

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

See `ARCHITECTURE.md`, `AGENTS.md` and `docs/adr/` before adding a module.


## License

DevBox Universal is distributed under the MIT License. See `LICENSE`.

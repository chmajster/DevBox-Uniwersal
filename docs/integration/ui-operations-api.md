# UI / Operations API integration contract

Agent 7 owns the web operations experience, host monitoring and the central log-source registry. It does not implement Git, runtime, Docker, database or Nginx lifecycle mechanisms.

## Existing Agent 7 endpoints

- `GET /api/v1/monitoring/snapshot`
- `GET /api/v1/monitoring/stream` (SSE)
- `GET /api/v1/logs/sources`
- `GET /api/v1/logs`
- `GET /api/v1/logs/stream` (SSE)
- `GET /api/v1/jobs/{id}/logs`
- `GET /api/v1/jobs/{id}/logs/stream` (SSE)

Built-in log sources are `devbox`, `job`, `project` and `deployment`. Provider modules may register additional implementations of `operations.LogSource`. Docker and Nginx integrations must expose logs through their own provider/module adapters rather than making Agent 7 execute Docker/Nginx commands directly.

## Project/UI endpoints consumed from domain modules

The frontend intentionally keeps these calls in `frontend/src/api/operations.ts` so route reconciliation after parallel-agent merges is isolated to one file.

Project module:

- `GET /api/v1/projects`
- `GET /api/v1/projects/{id}`
- `POST /api/v1/projects/{id}/actions/start`
- `POST /api/v1/projects/{id}/actions/stop`
- `POST /api/v1/projects/{id}/actions/restart`
- `POST /api/v1/projects/{id}/actions/deploy`
- `GET /api/v1/projects/{id}/open` -> `{"url":"..."}`
- `POST /api/v1/projects/{id}/terminal` -> controlled terminal session URL; never arbitrary shell input
- `GET /api/v1/projects/{id}/deployments`
- `GET /api/v1/projects/{id}/backups`

Docker module:

- `GET /api/v1/docker/status`
- `GET /api/v1/docker/containers`

Database module:

- `GET /api/v1/databases/status`
- `GET /api/v1/databases?project_id={id}`

Networking/proxy module:

- `GET /api/v1/networking/ports?project_id={id}`
- `GET /api/v1/proxy/status`

A missing parallel module is rendered as `UNHEALTHY` with the concrete API error. The UI does not invent success/failure state.

## Job progress payload convention

The existing Job entity remains unchanged. Producers can populate serializable `payload.stage`, `payload.progress`, `result.stage` and `result.progress`. The UI prefers result values and displays stage, numeric progress, elapsed time and durable logs. It never replaces progress with a spinner-only state.

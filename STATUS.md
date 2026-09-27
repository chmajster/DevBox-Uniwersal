# Status

Branch: `agent/06-networking`

Agent 6 — Networking / Nginx implementation is complete on the branch.

Implemented:

- SQLite-backed central Port Manager with allocate/reserve/release/inspect operations.
- Port collision checks against both durable DB leases and real host sockets.
- Reuse of released port records without losing the durable port identity.
- Nginx provider with detection, version, active-config validation, candidate testing, create/update/disable/delete and reload.
- Safe Nginx activation pipeline: render candidate → `nginx -t` candidate → atomic activation → full `nginx -t` → reload.
- Rollback of Nginx files/symlinks when active validation or reload fails.
- Hostname normalization and validation for names such as `cloudportal.devbox.local`.
- Windows, WSL and Linux hosts-file abstraction with no arbitrary privileged execution.
- Explicit manual host-entry instruction when the selected hosts file is not writable.
- HTTP and TCP health checks with persisted last status, response time, last error and timestamp.
- Domain CRUD and reverse-proxy API under `/api/v1`.
- Port listing/allocation/inspection/release API under `/api/v1`.
- Proxy status, candidate test and validated reload endpoints.
- Networking mutations integrated with RBAC and audit actor attribution.
- React pages `Domeny i Proxy` and `Porty` with allocation, CRUD, health and Nginx status/actions.
- Additive migration `002_networking_health.sql` for health response/error fields and health-check uniqueness.
- ADR-007 documenting port allocation, Nginx rollback and privilege behavior.

Tests added for:

- DB port collisions.
- Real socket collisions.
- Port release/reuse.
- Hostname validation.
- Nginx config generation.
- Failed Nginx candidate validation without reload.
- Failed full Nginx validation rollback.
- HTTP healthcheck timeout.
- TCP healthcheck.
- Managed hosts-file add/remove.

Validation:

- Backend and frontend CI quality gates are configured to run on `agent/**` branches.
- Final CI result is recorded in the pull request.

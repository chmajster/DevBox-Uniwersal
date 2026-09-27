# Status

Branch: `agent/01-foundation`

Foundation implementation is complete and validated.

Implemented:

- Go HTTP bootstrap and configuration loader.
- SQLite connection, WAL/foreign-key configuration and ordered transactional migration runner.
- Initial schema for users, sessions, projects, project sources, runtime configs, ports, domains, databases, database users, deployments, jobs, job logs, secrets, health checks, audit events and settings.
- Opaque-session authentication with bcrypt passwords and Admin/Operator/Viewer roles.
- Bootstrap administrator creation with explicit environment credentials.
- Core audit service and read endpoint.
- AES-256-GCM secret encryption abstraction and SQLite `SecretStore`.
- Runtime, job and provider contracts for parallel agent development.
- API v1 health, system info, auth, jobs and audit endpoints.
- React/TypeScript/Vite shell with login, protected layout, overview, jobs and audit pages.
- Reproducible Go/npm dependency locks.
- Backend/frontend GitHub Actions quality gates.

Validation:

- GitHub Actions run `36341280870`: successful.
- Backend: `gofmt` clean, `go vet ./...`, `go test ./...`, `go build ./cmd/devbox` all successful.
- Frontend: `npm ci`, lint, TypeScript typecheck, Vitest and Vite production build all successful.
- Tests cover idempotent SQLite migrations, API health + bootstrap-admin login/session/current-user flow, and AES-GCM secret round-trip without plaintext ciphertext leakage.

Intentionally not implemented in Agent 1:

- Full Git provider.
- Full runtime implementations.
- Docker provider.
- MySQL provider.
- Nginx/reverse-proxy provider.
- Platform-specific Windows/WSL service implementation.
- Production job worker implementations.

Those modules must implement the published contracts without creating cross-domain dependencies.

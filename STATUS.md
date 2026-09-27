# Status

Branch: `agent/02-projects-git`

Agent 1 foundation is merged into `main`. Agent 2 implements Project / Git / Deployment on top of the published contracts.

Implemented:

- Project CRUD with archive support and persisted configuration: source type, repository URL, branch, local path, runtime, deployment mode, working directory, build command, start command, healthcheck and auto start.
- Project sources: Git repository, existing local directory and empty project.
- Local-directory import with absolute/canonical path validation, symlink resolution, traversal protection and Git detection.
- Real Git CLI provider for clone, fetch, fast-forward-only pull, branch listing, checkout, status, current revision, remote, ahead/behind, dirty working tree and commit history.
- Private Git credentials through the existing encrypted `SecretStore`: GitHub token and SSH private key. Secret plaintext is not persisted in project configuration or logs.
- Git token injection through process environment/config rather than repository URL; temporary SSH key files use restrictive permissions and are removed after the command.
- Durable asynchronous `JobRunner` implementation for Git/deployment mutations, including queued/running/succeeded/failed/cancelled persistence, logs, restart recovery, cancellation and retry.
- Deployment workflow: `QUEUED -> PREPARING -> UPDATING_SOURCE -> DEPENDENCIES -> BUILDING -> STARTING -> HEALTHCHECK -> SUCCESS/FAILED`.
- Deployment history with commit before/after, start/end timestamps, duration, status/stage, error, triggering user and job identity.
- Project API under `/api/v1/projects`, including Git operations, import, archive, deployment and deployment history.
- React application management UI: application list, three-step add wizard, Overview/Git/Deployments/Configuration detail tabs and actions.
- Additive migration `002_projects_git_deployments.sql`.
- Tests for repository/branch/path validation, real clone and Git-state parsing, deployment state machine/failure persistence and secret masking.
- Frontend API client now handles non-JSON HTTP failures without throwing an unrelated JSON parse error.
- Authenticated state-changing requests use a double-submit CSRF token issued with the session and sent as `X-CSRF-Token` by the frontend client.

Provider boundaries:

- Agent 2 does not implement concrete application runtimes, Docker, MySQL or Nginx.
- Deployment uses the runtime registry contract. When a configured runtime is not registered, deployment is persisted as `FAILED` with `provider unavailable`.
- Docker deployment mode returns `provider unavailable: docker` until the Docker provider is available; no fake success is emitted.
- Git pull never performs `git reset --hard`.

Validation gates:

- Backend CI: `gofmt`, `go vet ./...`, `go test ./...`, `go build ./cmd/devbox`.
- Frontend CI: `npm ci`, lint, TypeScript typecheck, Vitest and Vite production build.

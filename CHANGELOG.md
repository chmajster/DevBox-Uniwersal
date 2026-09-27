# Changelog

## Unreleased

### Added

- Initial DevBox Universal repository architecture.
- Go backend bootstrap, environment configuration and graceful HTTP shutdown.
- SQLite bootstrap and ordered SQL migration engine.
- Complete initial schema for users, sessions, projects, project sources, runtime configs, ports, domains, databases, database users, deployments, jobs, job logs, secrets, health checks, audit events and settings.
- Authentication foundation with opaque sessions, bcrypt passwords and Admin/Operator/Viewer RBAC.
- Versioned `/api/v1` endpoints and normalized success/error envelopes.
- Audit event foundation.
- AES-256-GCM encryption and `SecretStore` abstraction.
- Job Engine and Runtime contracts.
- Git, process, Docker, database, reverse proxy, port allocator and system service provider contracts.
- React/TypeScript/Vite frontend shell, login flow and protected layout.
- Reproducible `go.sum` and `package-lock.json`.
- CI for backend and frontend quality gates.
- Architecture, roadmap, agent rules and ADR documentation.
- Project CRUD/archive service, SQLite repository and `/api/v1/projects` HTTP module.
- Git/local/empty project sources and safe existing-directory import.
- Real Git CLI integration for clone, fetch, fast-forward-only pull, checkout, branches, status, revision, remote, ahead/behind, dirty working tree and history.
- Encrypted GitHub-token and SSH-key credential references through the existing `SecretStore`.
- Durable asynchronous job runner with restart recovery, cancellation, retry and persistent job logs.
- Deployment state machine and deployment history with before/after commits, duration, error and triggering user.
- Application list, onboarding wizard and Overview/Git/Deployments/Configuration frontend views.
- Additive migration `002_projects_git_deployments.sql`.
- Agent 2 tests for Git validation/state/clone, path traversal, deployment transitions/failure persistence and secret masking.

### Changed

- Composition root now wires the project module, Git provider, runtime registry and durable job runner.
- Backend configuration adds `DEVBOX_PROJECTS_ROOT`.
- Frontend API client handles non-JSON error responses safely.
- Git SSH URL validation permits username-only SSH userinfo such as `ssh://git@host/repo` while rejecting embedded passwords/tokens.

### Security

- Repository URLs reject embedded HTTPS credentials and SSH passwords.
- Local project and working-directory paths are canonicalized and checked against traversal.
- Git token/private-key material is obtained from `SecretStore`, masked from command errors and never written into repository URLs.
- Temporary SSH key files are created with restrictive permissions and removed after use.
- Pull operations use `git pull --ff-only`; Agent 2 never performs `git reset --hard`.
- Missing runtime/Docker providers return explicit `provider unavailable` failures instead of fake success.
- Authenticated browser mutations require a double-submit CSRF token; login issues a readable CSRF cookie while the session cookie remains HttpOnly.

### Validation

- Foundation CI run `36341280870` passed all backend and frontend quality gates.
- Agent 2 frontend quality gates pass: install, lint, typecheck, tests and production build.
- Agent 2 backend validation is enforced by CI: formatting, vet, tests and build.

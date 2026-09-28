# Status

Branch: `agent/03-runtimes`

Agent 3 Runtime Engine implementation is complete and verified.

Implemented:

- Runtime registry with concrete Static, PHP, Python, Go and Node.js providers implementing the Agent 1 lifecycle contract.
- Deterministic project detection with runtime, framework, confidence, detected files, required version and suggested build/start commands.
- Host runtime inspection reporting `available`, `missing`, `invalid`, versions and component dependencies.
- PHP support for Composer, PHP-FPM, Laravel, Symfony, generic PHP and `ext-*` requirement validation without system-package auto-installation.
- Python per-project `.venv` isolation with pip, uv and Poetry support plus FastAPI, Flask and Django detection. Django `manage.py runserver` is restricted to development mode.
- Go dependency download and controlled `.devbox/build` binary output.
- Node.js npm/pnpm/yarn lockfile selection with install, build and start behavior.
- Built-in loopback-only static HTTP serving suitable for reverse-proxy handoff.
- Local process supervision for direct runtimes with project-scoped logs and lifecycle status.
- Deterministic Static restart that closes the bound listener before rebinding.
- Runtime configuration resolver isolated from ProjectService and backed by the Agent 1 project/runtime tables.
- SecretStore-backed environment resolution with API masking for secret references and secret-like environment keys.
- Runtime API module:
  - `GET /api/v1/runtimes`
  - `GET /api/v1/runtimes/detect?project_id=...`
  - `GET /api/v1/projects/{id}/runtime`
  - `POST /api/v1/projects/{id}/runtime/validate`
- Runtime Manager frontend and reusable project Runtime configuration section.
- Fixture-based detector tests for Static, Laravel, Symfony, FastAPI, Django, Go, Vite, pip, uv and Poetry.
- Lifecycle tests for Static restart, runtime log tail behavior and secret masking.

Scope exclusions preserved:

- No Docker implementation.
- No MySQL implementation.
- No Nginx implementation.
- No automatic operating-system package installation.

Validation:

- GitHub Actions run `36343299084` passed backend formatting, `go vet ./...`, `go test ./...` and `go build ./cmd/devbox`.
- GitHub Actions run `36343299084` passed frontend `npm ci`, lint, typecheck, tests and production build.

## Docker module

- Docker provider, Compose support and deployment-mode detection are implemented.
- Docker API is protected by RBAC and audited for privileged mutations.
- Frontend exposes Containers, Images, Volumes, Networks and Compose Projects.
- Agent 4 validation run: 36342877670.

## Database module

- MySQL/MariaDB provisioning, users, grants, backup/restore and phpMyAdmin are implemented.
- Project credentials are generated securely and persisted only through SecretStore.
- Agent 5 validation run: 36343337782.

## Networking module

- Port Manager, Nginx reverse proxy, domains and health checks are implemented.
- Nginx changes follow validate → activate → validate → reload with rollback on failure.
- Networking frontend exposes Domeny i Proxy and Porty.

## UI / Operations / Monitoring

- Monitoring and central operations log modules are registered alongside all existing domain modules.
- Dashboard, Applications, Project Details, Logs and enhanced Jobs UI are integrated.
- Theme switching and responsive operations layout are enabled.
- Agent 7 validation run: 36342945958.

## Projects / Git / Deployment

- Project management, Git integration and durable deployment jobs are integrated.
- Deployment jobs share the Runtime Engine registry instead of using a disconnected runtime registry.
- CSRF protection is enabled for authenticated state-changing requests.
- Agent 2 project UI is available under /apps alongside the Operations applications UI.
- Local-directory onboarding includes an Operator-only filesystem browser constrained to configured roots. Symlink targets are canonicalized, escapes are rejected, listings are capped, and browse operations are audited.

## Windows / WSL / Installer

- Windows PowerShell bootstrap and Linux installer are implemented.
- WSL/Linux platform and component detection are implemented.
- Read-only system platform/components API endpoints are authenticated with Viewer-or-higher RBAC.
- `devbox status` and `devbox doctor` are available.
- A narrow Linux `devbox-helper` privileged boundary is implemented with fixed allowlists.
- The Go server can serve the built frontend from `DEVBOX_FRONTEND_DIR`.
- Installer CI adds Bash syntax/shellcheck tests plus PowerShell parser tests.
- Agent 8 ADR is integrated as ADR-008 to avoid collision with the existing networking ADR-007.
## Integration fixes

- Applications use a single canonical `/apps` route; legacy `/applications` redirects to it.
- Frontend collection adapters tolerate `data: null` from empty API lists instead of dereferencing `items` on null.
- Light-theme module panels use theme variables instead of hard-coded dark backgrounds.
- Installed Nginx integration stores DevBox-managed site files under `/var/lib/devbox/nginx` and performs global validation/reload through the allowlisted privileged helper.


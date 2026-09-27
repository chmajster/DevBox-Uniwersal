# Status

Branch: `agent/03-runtimes`

Agent 3 Runtime Engine implementation is complete pending final CI verification.

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
- Runtime configuration resolver isolated from ProjectService and backed by the Agent 1 project/runtime tables.
- SecretStore-backed environment resolution with API masking for secret references and secret-like environment keys.
- Runtime API module:
  - `GET /api/v1/runtimes`
  - `GET /api/v1/runtimes/detect?project_id=...`
  - `GET /api/v1/projects/{id}/runtime`
  - `POST /api/v1/projects/{id}/runtime/validate`
- Runtime Manager frontend and reusable project Runtime configuration section.
- Fixture-based detector tests for Static, Laravel, Symfony, FastAPI, Django, Go, Vite, pip, uv and Poetry.

Scope exclusions preserved:

- No Docker implementation.
- No MySQL implementation.
- No Nginx implementation.
- No automatic operating-system package installation.

Validation:

- Final GitHub Actions backend/frontend quality gates will be recorded after the branch push.

# Status

Branch: `agent/08-installer-wsl`

Agent 8 Windows / WSL / Installer implementation is complete and ready for integration.

Implemented:

- Root `install.ps1` for Windows/WSL discovery, supported-distribution selection, optional Ubuntu installation, WSL systemd enablement, invocation of the Linux installer and GUI/API verification.
- Root `install.sh` with `--install`, `--status`, `--repair`, `--update`, `--uninstall`, `--help` and optional `--purge` for full data removal.
- Eight-stage Linux installer output with `[ OK ]`, `[INFO]`, `[WARN]`, `[FAIL]`; ANSI is emitted only to an interactive terminal and never written to installer logs.
- Detection for Git, Docker, Nginx, MySQL/MariaDB, PHP, Composer, Python, pip, Go, Node.js and npm.
- Linux/WSL platform detection including distribution metadata, WSL generation and systemd state.
- Authenticated read-only System Components API at `/api/v1/system/components` and platform API at `/api/v1/system/platform`.
- Privilege-separated `devbox-helper` with a fixed operation surface for whitelisted package installation, whitelisted service restart, validated Nginx reload and validated DevBox environment-file writes. No arbitrary command execution operation exists.
- `devbox status` and `devbox doctor` CLI commands.
- Doctor checks for SQLite, migrations, filesystem write access, Nginx binary/service state, Docker daemon, MySQL/MariaDB service, runtime binaries, DevBox HTTP port and DevBox systemd service.
- systemd service installation under an unprivileged `devbox` account.
- Built frontend serving from the Go process when `DEVBOX_FRONTEND_DIR` is configured, allowing the installer to expose one GUI/API address (`http://localhost:8787/`).
- Uninstall preserves `/var/lib/devbox` by default; `--uninstall --purge` / `-Mode Uninstall -Purge` removes data and the system account.
- Installer parser/idempotency tests, WSL/os-release parser tests, component detection tests, helper whitelist tests and SPA serving tests.
- CI validation for Go, shell syntax, shellcheck, shell installer tests and PowerShell parser/tests.

Validation performed before commit:

- `gofmt` on Agent 8 Go files: clean.
- `go test ./internal/system ./internal/webui ./cmd/devbox-helper`: successful in the isolated Agent 8 validation module.
- `bash -n install.sh scripts/test-install.sh`: successful.
- `scripts/test-install.sh`: successful.

Validation delegated to GitHub Actions because the execution environment has no outbound access to clone the repository and does not provide local `shellcheck` or `pwsh` binaries:

- full `go vet ./...`, `go test ./...`, `go build ./cmd/devbox ./cmd/devbox-helper`;
- frontend lint/typecheck/test/build;
- shellcheck;
- PowerShell AST validation and parser tests.

Integration notes:

- No shared provider contract was changed.
- Component installation over HTTP was intentionally not added: long-running privileged mutations must go through the Job Engine once its durable worker exists.
- `devbox-helper` is a narrow privileged boundary and never accepts an arbitrary executable, argument vector or target path.

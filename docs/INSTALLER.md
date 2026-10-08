# Installer and WSL operations

## Windows

From an elevated PowerShell in the repository root:

```powershell
.\install.ps1 -Mode Install
```

If no supported WSL distribution exists, allow Ubuntu installation explicitly:

```powershell
.\install.ps1 -Mode Install -InstallUbuntu
```

A specific existing Ubuntu/Debian distribution can be selected with `-Distribution`.

Other modes map directly to the Linux installer: `Status`, `Repair`, `Update`, `Reinstall`, `Uninstall`. Full removal requires `-Mode Uninstall -Purge`; without `-Purge`, application data is retained.

## Linux / WSL

```bash
sudo ./install.sh --install
./install.sh --status
sudo ./install.sh --repair
sudo ./install.sh --update
sudo ./install.sh --reinstall
sudo ./install.sh --uninstall
sudo ./install.sh --uninstall --purge
```

The installer supports Ubuntu and Debian. User applications require Docker Engine/Compose, not host PHP/Composer/Python runtimes. Go and Node/npm in the source installer build DevBox itself; application versions and dependencies remain in images. It installs only a fixed package list, builds the Go backend and React frontend, installs the `devbox` systemd service and exposes the combined GUI/API on `http://localhost:8787/`.

`devbox status` reports platform and component state. `devbox doctor` performs operational diagnostics without exposing a generic root shell. Git-based systemd updates persist their live state in `/var/lib/devbox/update-status`; the API exposes it at `GET /api/v1/update/progress`, including percentage, stage, timestamps and failure state. Failed runs also expose the exit code and a bounded, sanitized tail of `/var/log/devbox-update.log` to authenticated administrators so the exact failing command output can be diagnosed from the UI. Common password/token/authorization patterns are redacted before log lines are returned. The file is updated atomically and remains available across the intentional `devbox.service` restart.

## Privileged helper

`devbox-helper` is intentionally narrow. Supported operation families are:

- install one component mapped to a whitelisted Debian package;
- restart one whitelisted service;
- validate and reload Nginx;
- start the fixed `devbox-update.service` systemd unit without arbitrary command execution;
- write the controlled DevBox environment file after key/value validation.

There is no `exec`, `shell`, raw command, raw path or arbitrary package operation.

## Reinstall and Docker diagnostics

`--reinstall` / PowerShell `-Mode Reinstall` replaces DevBox software and preserves SQLite, the encryption key, SecretStore, environment configuration, sources and SQL Docker volumes. Only `--uninstall --purge` requests removal of persistent DevBox state. SQL volumes are not implicitly destroyed by application deletion or software reinstall.

The installer checks Docker Engine, Compose and daemon access and adds the service account to the Docker group. `devbox doctor` checks Compose and the configured shared application network; deployments create that network if absent. Installed application runtimes and SQL administration live in containers. The installer builds and installs the static `devbox-dbcheck` SQL probe alongside DevBox; a source checkout can set DEVBOX_DATABASE_PROBE to its compiled location.

For Windows source directories configure DEVBOX_DIRECTORY_BROWSE_ROOTS with the corresponding `/mnt/c/...` roots visible inside the selected WSL distribution. Select a specific application directory, not the whole drive. DevBox resolves symlinks and validates the Linux path before mounting it. Test both a host edit and a container write; use WSL metadata/UID ownership where required by your filesystem settings. No global chmod 777 is applied.

A real reinstall of an existing service is intentionally separate from repository validation. Bash/Powershell parser tests verify installer logic; the Docker E2E suite verifies actual `/mnt/c` application reads and writes when DEVBOX_TEST_WSL_ROOT is set.
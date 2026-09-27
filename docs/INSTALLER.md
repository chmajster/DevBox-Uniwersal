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

Other modes map directly to the Linux installer: `Status`, `Repair`, `Update`, `Uninstall`. Full removal requires `-Mode Uninstall -Purge`; without `-Purge`, application data is retained.

## Linux / WSL

```bash
sudo ./install.sh --install
./install.sh --status
sudo ./install.sh --repair
sudo ./install.sh --update
sudo ./install.sh --uninstall
sudo ./install.sh --uninstall --purge
```

The installer supports Ubuntu and Debian. It installs only a fixed package list, builds the Go backend and React frontend, installs the `devbox` systemd service and exposes the combined GUI/API on `http://localhost:8787/`.

`devbox status` reports platform and component state. `devbox doctor` performs operational diagnostics without exposing a generic root shell.

## Privileged helper

`devbox-helper` is intentionally narrow. Supported operation families are:

- install one component mapped to a whitelisted Debian package;
- restart one whitelisted service;
- validate and reload Nginx;
- write the controlled DevBox environment file after key/value validation.

There is no `exec`, `shell`, raw command, raw path or arbitrary package operation.

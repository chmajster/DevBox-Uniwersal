# ADR-007: Windows/WSL installer and privileged helper

Status: Accepted

## Decision

Windows installation is a bootstrap layer around WSL. `install.ps1` selects a supported Ubuntu/Debian WSL distribution and delegates Linux-side installation to `install.sh`. The installed API process runs as the unprivileged `devbox` account.

Privileged host mutations use `devbox-helper`, whose command surface is a fixed set of typed operations. The helper does not accept an arbitrary executable, shell command or destination path. Package and service names are mapped through internal allowlists. Nginx validation and reload are explicit helper operations; the installer grants the `devbox` service account passwordless sudo only for those exact helper commands.

The HTTP System Components API is read-only until the durable Job Engine worker exists. Privileged package installation must not be implemented as a blocking root operation inside an HTTP handler.

## Rationale

WSL is the Linux execution boundary on Windows, so maintaining separate Windows implementations of Git, Nginx, MySQL and application runtimes would duplicate providers and produce different behavior. Delegating to the Linux installer keeps the control plane consistent.

A network-facing process must not run permanently as root. A constrained helper minimizes the escalation surface and follows ADR-006.

## Consequences

- Initial installer support is limited to Ubuntu and Debian WSL distributions.
- WSL systemd is enabled when required for install/repair/update and WSL is restarted.
- Uninstall preserves `/var/lib/devbox` by default; purge is explicit.
- Future UI/API component installation must enqueue audited Job Engine operations that invoke the same typed privileged boundary.

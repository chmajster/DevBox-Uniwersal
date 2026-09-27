# ADR-007: Networking, port allocation and Nginx activation safety

Status: Accepted

## Decision

The networking module owns SQLite-backed port leases, domain mappings, network health checks, hosts-file reconciliation and the concrete Nginx reverse-proxy provider.

Port allocation checks durable leases before probing the host socket and persists every lease state. Nginx site changes are rendered to a candidate configuration, validated with `nginx -t`, activated atomically, validated again as part of the complete Nginx configuration and only then reloaded. Failed validation or reload restores the previous files. In installed deployments, mutable DevBox site files live under `/var/lib/devbox/nginx`; a root-owned Nginx include loads them, while global validation and reload are delegated to the allowlisted `devbox-helper` through non-interactive sudo.

Hosts-file changes never invoke elevation or arbitrary privileged commands. When the selected hosts file is not writable, the API returns the exact required manual change.

## Rationale

Port and proxy state must be deterministic across restarts, while Nginx must never be reloaded with a configuration known to be invalid. The control-plane process remains unprivileged in accordance with ADR-006.

## Consequences

The default Nginx paths target a conventional Linux/WSL installation and are configurable through environment variables. Windows/WSL hosts-file handling is file-based and reports privilege requirements instead of escalating.

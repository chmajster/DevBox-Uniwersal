# ADR-014: One hosting application model, two Docker modes

Status: superseded by [ADR-015](015-docker-hosting-and-shared-sql.md). Historical decision; the generated/Compose engines, durable jobs, secrets and provider boundaries remain in use.

The user selects a source directory and explicit runtime/version/port/command. The only execution modes are source-owned Docker Compose and a generated Managed runtime. All application processes execute in Docker. A central catalog supplies version choices to API/UI. Code is mounted RW from its original location, with source UID/GID for supported processes. Generated artifacts are private and outside source.

Application topology, public env, encrypted secret bindings, port leases and domain routes have separate persistent ownership. Asynchronous jobs coordinate build/rebuild/recreate/lifecycle. Runtime state comes from Docker reconciliation and readiness/publication checks. TLS belongs to domains in the central validated Nginx manager. Default deletion preserves user sources and named data volumes.

Remove competing host runtime processes, script-installed applications and single-image/Dockerfile drivers. A custom Dockerfile remains usable through source Compose. Independent infrastructure/database modules retain the tables they still use; they do not deploy user apps through a second engine.

Managed replacement retains a rollback path where practical. Source Compose updates remain nontransactional. Existing certificates are supported; ACME is disabled until an issuer/challenge manager is configured. See [the hosting guide](../application-control-plane.md) for operation semantics and verification.

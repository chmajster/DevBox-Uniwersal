# ADR 012: Application, workload and endpoint ownership

Status: Accepted for PR #140

## Decision

Replace the workspace's Project lifecycle with an Application aggregate. Sources, runtime descriptions, workloads, endpoints, deployment attempts and actual runtime observations have independent persistence and APIs. Keep existing global infrastructure providers rather than duplicating Docker, runtime generation or encrypted secret storage.

Deployment drivers implement Detect, Plan, Deploy, Inspect and lifecycle operations. Selection favors explicit declarations over heuristics. Ambiguity is configuration work, not a reason to publish MySQL/Redis through an HTTP endpoint.

Persist application ownership on jobs and enforce one active mutation per application with a SQLite unique index plus a service mutation lock. Do not release cancellation ownership before rollback completes. Configuration changes and explicit retry use the same guard. Record the retrying actor.

Stage new topology without dropping previous runtime identities before successful execution. Preserve observed host ports separately from requested plan ports. Reconciliation derives status from workloads and invalidates stale observations on provider failure, never from the outcome of the last deployment alone.

Applications with provisioned workloads may save a different deployment driver. The saved driver remains pending while the current driver continues to own live resources and lifecycle actions. Driver switches stop the previous workloads to release conflicting published ports; if the new deployment fails, DevBox attempts to restart them. A successful deployment activates the new driver, stores its runtime, and asks the previous driver to remove only superseded resource identities. Cleanup failures are reported as deployment warnings; Compose remains nontransactional and may require operator recovery.

## Consequences

The workspace/API switches to `/applications`. Released Project records remain untouched; this refactor does not promise automatic backfill or feature-for-feature compatibility with project-specific extension modules. Migration is additive, but operators must explicitly onboard application sources.

Secrets are application-scoped and encrypted by the existing SecretStore; only names are readable. Sources and persistent volumes are preserved by default and cannot be erased incidentally by deleting a configuration.

Compose is source-owned and nontransactional. A failed stack update may require operator recovery; never advertise an atomic rollback or zero-downtime replacement. DNS/TLS routing, automatic database-binding adoption and full legacy module migration remain outside this version.

See `docs/application-control-plane.md` for operational/API semantics and the real Docker test scope.

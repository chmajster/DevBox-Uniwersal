# ADR-012: deployment, jobs and integration hardening

Status: accepted.

## Context
The audit found ignored build/start commands, masked image artifacts, unsafe
empty network handling, non-durable plugin installs and ambiguous runtime state.

## Decisions
- Managed runtime options are additive JSON configuration. Live source remains
  the default. Versioned mode uses the sanitized image snapshot, with no source
  bind mounts; it provides code rollback but never rolls back database contents.
- Build/start commands are interpreted only within the managed image/container.
  Custom Dockerfiles and Compose remain authoritative and are never rewritten.
- Generated artifacts have image-initialized volumes in live mode. UID/GID and
  required writable paths are explicit; no recursive chmod of host sources.
- Readiness is checked against the published application port, with a configured
  HTTP path or TCP and bounded deadlines. Provider progress is reported through
  a context-scoped observer, not persisted callbacks or provider dependencies.
- Jobs have durable resource keys and a bounded worker pool. Active keys stay
  locked until handlers exit, including after cancellation. Restart recovery
  requeues only explicitly idempotent handlers; other operations require review.
- Host network diagnostics do not need or store SQL credentials. Database user
  selection is explicit and protected from deletion while a binding references it.
- Package installation, runtime image changes and TLS provisioning are audited
  jobs. Providers remain responsible for their own external execution boundary.

## Consequences
Configuration and status are explicit and testable. Existing configuration uses
safe defaults; migrations are additive. Live mode cannot promise a code rollback.
A running application remains running in inventory even if a post-activation
control-plane write fails; the deployment still records that failure separately.

## Trust and recovery boundaries

Signed source archives use an out-of-band Ed25519 public key; only explicitly
selected development mode accepts unsigned Git. Control-plane rollback snapshots
are encrypted with the existing master key. Application data and host package
transactions are outside this snapshot. Job admission and HTTP mutations are
blocked while a consistent pre-migration snapshot is taken.

Local HTTPS uses SecretStore-backed CA/leaf keys and a verified Nginx listener.
The public CA must be trusted explicitly on intended development clients.
The application never installs trust roots on clients or impersonates public CA trust.

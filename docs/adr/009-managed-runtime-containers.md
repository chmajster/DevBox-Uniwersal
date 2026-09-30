# ADR-009: Managed runtime containers

## Status

Accepted.

## Context

DevBox previously supported a native application deployment path where PHP, Python, Go and Node.js dependencies could be validated, installed, built and started directly on the host. That model coupled application requirements to the DevBox host, made conflicting runtime versions difficult to isolate and allowed one project's system dependencies to affect another.

Projects may also already contain their own Dockerfile or Compose definition. Those definitions must remain authoritative rather than being replaced by generated configuration.

## Decision

Docker is the mandatory execution boundary for managed applications. There is no native host-process application deployment mode.

For each deployment DevBox resolves the container source in this order:

1. use a project-owned Compose definition when present;
2. otherwise use a project-owned Dockerfile when present;
3. otherwise, for container policy `auto`, generate a managed image for PHP, Node.js, Python, Go or static content;
4. container policy `custom` requires a project-owned Compose definition or Dockerfile.

Runtime selection, runtime version and image modules are configured per project. Modules are selected from runtime-specific allowlists. The API does not accept arbitrary OS package names or shell fragments as module definitions.

For generated PHP images DevBox may also infer modules from Composer platform requirements. Only `ext-*` requirements that map to the existing allowlisted PHP module catalog are added automatically, and explicit project module selections remain additive. Manifest inference never permits arbitrary package names or shell fragments.

Generated build contexts are staged in a temporary directory and exclude `.env*`, `.git`, dependency directories, virtual environments, IDE metadata and common local caches. Secrets are injected only at runtime through existing secret/environment mechanisms and are not embedded in generated Dockerfiles.

Managed images use deterministic fingerprints derived from runtime, version, selected modules, source revision and sanitized application content. A build is skipped when the persisted fingerprint and image are unchanged unless a forced rebuild is requested.

Managed container replacement keeps the previous container until the new one starts and passes its HTTP health check. Failure removes the failed replacement and restores the previous container. Managed containers use `no-new-privileges`, reduced capabilities and non-root generated images where the runtime permits it.

Host ports are allocated by the central PortAllocator and successful applications are attached to the existing reverse-proxy layer.

## Consequences

- Docker Engine is required to execute applications managed by DevBox.
- PHP-FPM, PHP extensions, Go, Node.js and Python no longer need to be installed on the host for project execution.
- Applications can use different runtime versions and module sets without polluting the host.
- Existing database rows are migrated to Docker deployment semantics by migration 007.
- The legacy `deployment_mode` column remains in the schema because released migrations are immutable, but the supported application execution semantics are Docker-only and new project APIs do not expose a native mode.
- Compose projects retain their own service topology and lifecycle semantics.

# Docker module

The Docker module implements the shared `providers.DockerProvider` contract using controlled `docker` / `docker compose` CLI invocations. It never invokes a shell and never accepts raw CLI argument arrays from HTTP.

Additional module-owned capabilities include container restart and allow-listed exec diagnostics, image/volume/network inventory and inspection, supported Compose file discovery, Compose validation/pull/build/up/down/restart/logs/ps, and Docker-backed status reporting.

Compose projects exposed through HTTP are resolved as validated child directories of `DEVBOX_PROJECTS_ROOT`; callers cannot supply arbitrary filesystem paths. The project/deployment module can call `Create` for Docker mode or the Compose methods for DockerCompose mode without importing another provider implementation.

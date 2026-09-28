# TODO

Current remaining work:

- Add login rate limiting, active-session administration and global session revocation.
- Add master-key versioning and online secret rotation.
- Publish signed/versioned release artifacts with checksums and rollback-capable updates.
- Add end-to-end browser tests for install → login → project onboarding → runtime → proxy → health → delete.
- Extend runtime management with per-project version selection/installation.
- Add automatic local HTTPS certificate provisioning and lifecycle.
- Add complete DevBox configuration/database/project metadata backup and restore.
- Add application templates for common stacks.
- Add remote DevBox Agent nodes with authenticated controller/agent transport.
- Add finer-grained permissions beyond Admin/Operator/Viewer when multi-user deployments require them.

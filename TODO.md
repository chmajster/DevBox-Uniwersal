# TODO

Current remaining work:

- Add login rate limiting, active-session listing/inspection and global all-users session revocation. Per-user session revocation is implemented.
- Add master-key versioning and online secret rotation.
- Configure the operator signing identity and publish signed release tags; signed verification/rollback tooling is implemented.
- Add end-to-end browser tests for install → login → project onboarding → runtime → proxy → health → delete.
- Extend runtime image lifecycle with storage budgets and retention policies.
- Add public-domain ACME and optional platform-specific client trust onboarding; local TLS lifecycle is implemented.
- Add application templates for common stacks.
- Add remote DevBox Agent nodes with authenticated controller/agent transport.
- Add finer-grained permissions beyond Admin/Operator/Viewer when multi-user deployments require them.

# ADR-010: Configurable container port publishing

Status: Proposed.

## Context

An application listener and its published host port are different settings. The old managed deployment path allocated a single generic host port, used only the first Dockerfile EXPOSE instruction (or a generated fixed 8080 listener), and could not configure a second HTTPS mapping. Project listing selected the most recent lease without distinguishing HTTP from HTTPS.

## Decision

- Add per-project desired settings and last-applied mappings in `project_port_publishing`, through additive migration 008. GET/PUT `/api/v1/projects/{id}/ports/config` use the existing authentication, roles, request validation, response envelope and audit service. Configuration writes do not mutate running containers; deployment remains a durable job. Active deployments prevent conflicting configuration writes.
- New managed HTTP publications start at 8080. Optional HTTPS passthrough starts at 8443 and defaults to internal port 443. Internal HTTP port 0 means EXPOSE/generated-runtime detection. Existing applications retain their previous port until explicitly reconfigured.
- `SequentialPortAllocator.ReserveFrom` tries the requested number and then +1 through 65535. Exact `PortAllocator.Reserve` remains unchanged. The optional result `PortReservation` identifies reused leases; the `PortLeaseOwner` contract makes reuse and rollback ownership-aware. Unique SQLite reservations arbitrate concurrent DevBox projects, while IPv4/IPv6 socket probes detect host listeners. Non-collision errors are reported rather than skipped.
- Successful deployment persists the resolved host numbers as the project's editable settings. This both keeps ordinary redeploys stable and permits an explicit later move back to a now-free lower port. A failed deployment retains the previous applied mapping and leases. Ports serving a replacement, or whose cleanup could not be confirmed, are not released to other projects.
- The primary project port is the application/HTTP lease, never its additional HTTPS lease. Reverse proxy routing and readiness checks target that resolved HTTP publication.
- Generated HTTP listener changes update the generated Dockerfile, runtime environment and image fingerprint. Host publication does not enter the image fingerprint. Custom Dockerfiles and application code are not rewritten. Existing live source mounts and anonymous dependency volumes are preserved.
- HTTPS is passthrough, not certificate provisioning or TLS termination. The initial UI permits it only for project-owned Dockerfile/Compose deployments. Generated runtime images remain HTTP-only.
- Saving settings opts project-owned Compose into port management. Only the selected service's published ports are replaced, using a minimal `!override` file. Compose 2.24.4+ is required for this opt-in path; unconfigured Compose projects preserve their own topology and existing legacy compatibility. Multiple possible web services require an explicit selection. Host/container networking and profile-gated web services are rejected rather than silently misconfigured.
- Compose state is kept outside application source: `DEVBOX_COMPOSE_PORTS_DIR`, or the service user's configuration directory under `devbox/compose-ports`. Overrides contain only a validated service name and numeric TCP mappings, never the full interpolated Compose output or secrets. They are written atomically and restored on validation/deployment failure. All Compose lifecycle commands use the same durable override.

## Consequences

Application authentication, firewall rules, router/NAT/WSL forwarding, Docker's host binding policy, TLS certificates and custom application listeners remain explicit operator responsibilities. A socket probe cannot lock out an unrelated OS process indefinitely; Docker binding remains the final check and a binding failure fails the deployment safely. Compose rollback restores port configuration, not arbitrary application source or previous image contents.

## Verification

Unit/regression coverage includes ranges, defaults, consecutive/cross-project allocation, ownership, restart persistence, IPv6 occupancy, invalid-input/busy API responses, generated/custom listeners, fingerprints, read-only UI, URL construction and override rollback. `DEVBOX_TEST_DOCKER_PORTS=1 go test ./internal/docker -run TestPublishedPortsDockerIntegration -v` additionally exercises real Docker publication, a non-default nginx listener, multiple published ports, live source edits and Compose remapping.

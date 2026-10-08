# ADR-015: Docker hosting and application-native shared SQL

Status: accepted. Supersedes the two user modes in ADR-014 and the WordPress-only binding scope in ADR-013.

Application is the sole lifecycle entity. The user has three execution choices: existing Compose, existing Dockerfile and generated Docker. OCI is a source exception without a filesystem mount. The existing Compose and managed engines implement these choices; there are no revived native host processes or separate Dockerfile/image lifecycle systems.

All source-based development modes use a real RW bind at a compatible working directory. Generated PHP uses Apache and source-owner workers; Go builds and runs a binary from its live source. Dependencies remain accessible in the image or dedicated volumes. Runtime choices come from one backend catalog and image availability is checked before deployment.

Private application networks coexist with one shared SQL network. Independent MySQL, MariaDB and PostgreSQL servers keep named persistent volumes. Application bindings record an exact database and account, with encrypted password references. Provisioning and SELECT 1 from the actual workload run as durable application jobs. PostgreSQL PUBLIC access is revoked; network reachability does not grant SQL privileges. Legacy project SQL deployment mutations are retired while historical records remain readable.

HTTP readiness is distinct from Docker process state. Durable host-port leases check socket listeners and Docker publications. Managed replacement restores the previous container on startup failure. Compose command/port failure attempts previous-plan recovery using retained images and data; source changes and external resources remain nontransactional.

Installer Reinstall replaces software and preserves data, keys, configuration and volumes. Generated artifacts exclude credentials, ownership checks protect foreign resources, and application Compose cannot request privileged/host mounts or build escapes. See the [hosting guide](../application-control-plane.md) and [executed verification](../../STATUS.md).

# Audit completion: B01–B06, F01–F09, D01–D05

Scope is the numbered audit requested on 2026-09-29. The ambiguous original
“0 do 6b” is implemented as B01–B06; B07/B08 and D06 onward are not silently
claimed as completed. Based on main after the merge of PR #89 (`f69cdc18`).

| ID | Implemented contract | Verification |
|---|---|---|
| B01 | Empty/one/multiple network handling; panic boundary; previous container preserved | Replacement regression tests; real Docker CI scenario |
| B02 | Build/start commands included in generated image and fingerprint; unsupported overrides rejected | Generator tests for all five runtimes; real Node build/start scenario |
| B03 | Live Node image artifacts initialized in dedicated volumes, separate from host bind | Real Docker test starts from source with no host `dist` |
| B04 | Explicit `live`/`versioned` mode; versioned uses immutable image source for rollback | Docker CI modifies source then rejects broken replacement and verifies old response/image |
| B05 | Configured HTTP path/query/statuses or TCP, actual published port, bounded startup and request timeouts | HTTP test server plus real Docker readiness path distinct from `/` |
| B06 | PHP, extensions, Compose, PostgreSQL and host MySQL installs use durable jobs | Existing helper/plugin tests; role-gated HTTP returns job; persisted UI logs |
| F01 | Separate asynchronous DNS/TCP check without resolving/decrypting SQL credentials | Runtime interpreter or isolated helper in actual application network namespace; SQL test remains separate |
| F02 | MySQL daemon/default options port detection, TCP readiness, Docker-reserved port conflict | MySQL/MariaDB and reserved-port regression tests preserved from PR #89 |
| F03 | Explicit persisted database user ID, ownership validation, bound-user deletion protection and selected-user rotation | Database tests; additive FK migration |
| F04 | Non-root UID/GID and required writable directories, real read/write preflight | Generator/runtime access tests; no host `chmod 777` or recursive ownership change |
| F05 | CGO switch with matching Debian dynamic runtime; unsupported path combinations rejected | Generated image regression checks |
| F06 | Restart reconciliation only for explicitly recoverable jobs, completed-clone recognition | Restart and panic tests; unsafe jobs marked for inspection, not replayed |
| F07 | Configurable 1–16 workers (default 4), resource keys and global maintenance exclusion | Concurrency, same-resource, global-lock and cancellation tests, race CI |
| F08 | Provider reports healthcheck stage before readiness wait begins | Managed replacement observer integrated into deployment stage persistence |
| F09 | Runtime activation tracked separately from final deployment result | Post-activation persistence error no longer blindly reports a stopped application |
| D01 | Main's MySQL plugin and host/managed phpMyAdmin support retained; installs/actions made durable | Main PR #89 merged independently; its tests retained; host reserved-port safeguards preserved |
| D02 | Registry version catalog, installed-image inventory/project use, job-based pull/safe remove | Pagination, token audience, cache and version mapping tests |
| D03 | GitHub/GitLab profiles using existing SecretStore tokens, repositories, branches, PR/MR and CI views | URL validation, GitLab subpaths, no-follow redirects and token-clearing tests |
| D04 | Ed25519 signed source artifacts, digest verification, encrypted pre-migration snapshot, version+SQLite postcheck, rollback | Signature/tamper/archive, snapshot/restore, idle queue and exact-version health tests |
| D05 | Local CA, domain TLS issuance/renewal/disable/verify, Nginx termination, public CA export | X.509 chain/hostname/key identity, encrypted storage, permissions and renderer tests |

## Runtime and jobs

`execution` is stored with the runtime configuration. Existing projects remain in
Live Code mode. A versioned deployment has no mutable source bind; it preserves
code in the previous image, **not database writes or external data**. Project-owned
Dockerfiles/Compose remain authoritative. For Go custom builds the executable
must be written to `/out/app`; use a custom Dockerfile for arbitrary data paths.

Use `DEVBOX_JOB_WORKERS=4` (range 1–16). Resource ownership lasts until a cancelled
handler actually exits. Restarted non-idempotent jobs are marked failed with a
reconciliation message. The worker pool is in one control-plane process; running
multiple DevBox processes against the same installation is not supported.

Host access stores only engine/endpoint. Network diagnostics require an already
running, uniquely identified application container. They do not create users,
open firewall rules, alter MySQL bind-address/grants, or request a password.
An optional diagnostic helper image is downloaded inside the durable job, not
inside the HTTP request. A missing Docker daemon/image/network is reported as an
operation error, never as successful connectivity.

## Signed updates: deployment prerequisites

The default update mode is `signed`. A missing trusted public key is an explicit
not-ready state; the updater does **not** silently fall back to unsigned Git.

Generate one signing key pair on a trusted administration machine:

```sh
cd backend
go run ./cmd/devbox-release --generate-key --output-dir /secure/devbox-signing
```

Keep the private key out of Git and host application directories. Configure its
base64 content as the repository Actions secret `DEVBOX_RELEASE_SIGNING_KEY`.
Install the generated public key on each DevBox host, root-owned, without group
or other write permissions, and configure:

```ini
DEVBOX_UPDATE_MODE=signed
DEVBOX_UPDATE_PUBLIC_KEY=/etc/devbox/update-public.key
DEVBOX_UPDATE_RELEASE_BASE=https://github.com/chmajster/DevBox-Uniwersal/releases/latest/download
```

Creating a semantic tag such as `v1.0.0` runs `signed-release.yml`. It publishes the
source archive, signed manifest and detached signature only when the signing key
is configured. This implementation does not create a signing identity, configure
repository secrets or publish a release on behalf of the operator.

Before installation, the updater authenticates the manifest/archive, rejects an
older signed release date, blocks new mutations/jobs, confirms the queue is idle,
stops the service and snapshots control-plane files/SQLite. Snapshots are encrypted
with the **existing master key** and stored root-only under
`/var/backups/devbox-updates`. Keep that original key available outside the host.

The final check requires the expected installed SHA and a healthy SQLite database.
An installation/health failure restores the saved binary, configuration and SQLite,
then checks the previous version. An interrupted update retains a recovery marker;
`sudo /usr/local/lib/devbox/devbox-updater.sh --recover` (or the next updater run)
reconciles it. Host apt changes, Docker images, application volumes and external
SQL databases are not included in the control-plane rollback. Backup retention is
operator-managed; snapshots are not silently pruned.

For explicitly selected development-only unsigned updates, both
`DEVBOX_UPDATE_MODE=git` and `DEVBOX_UPDATE_ALLOW_UNSIGNED=true` are required.
The UI labels this mode as unsigned. It still takes a rollback snapshot.

## Local HTTPS: deployment prerequisites

Domain actions create a local development CA in SecretStore, issue 90-day leaf
certificates, configure Nginx on port 443 and redirect HTTP to HTTPS. Renewal is
checked on startup and daily for certificates expiring within 30 days. Activation
must present the expected certificate through a real TLS handshake before it is
reported as verified. Failed activation restores the committed route.

Only localhost, private IPs and `.localhost`, `.test`, `.local`, `.internal`, `.lan`
hostnames are eligible. Public-domain ACME is not implemented. The browser/client
must explicitly trust the **public** CA exported by the panel; private CA keys are
never exported. Windows trust is separate from WSL/Linux trust. Nginx availability,
DNS/hosts resolution, port 443, firewall/NAT and client trust remain host prerequisites.

## Test boundaries

Local verification runs the complete Go suite, vet/build, frontend lint/typecheck/
unit tests/build and installer parser/idempotency tests. `audit-deployment-e2e` in
GitHub Actions runs the actual Docker generator/build/replacement/rollback and job
race tests. Mock transports are confined to tests; application provider endpoints
perform real operations. No test run here is a claim that a user's installed host,
GitLab instance, signing secret or browser trust store has been configured.

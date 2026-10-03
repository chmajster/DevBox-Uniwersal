# Application control plane

The `/apps` workspace uses `/api/v1/applications`. An **Application** owns its source and desired state; **Workloads** represent individual containers/services; **Endpoints** represent listeners and observed host-port mappings. Deployment history is separate from runtime state: an unsuccessful deployment does not by itself mean the previous container stopped.

## Workflow

1. Add Git, an allowlisted local directory, a Docker/OCI image, or an empty source. Git URLs accept saved credential IDs, not embedded passwords. Local paths refer to the DevBox host/WSL filesystem.
2. Optionally analyze the source. Git analysis is a durable background job using a temporary clone, removed afterward. It does not deploy containers.
3. Save the application, add required encrypted secrets, then deploy. The driver can be selected in application configuration before the first deployment; choose automatic detection, a generated runtime container, Dockerfile, image or the source-owned Docker Compose stack. An ambiguous port/driver becomes `waiting_for_configuration`, not a successful deployment or a generic infrastructure failure. Edit configuration and deploy again.
4. Inspect services, endpoints, deployment history, logs, secrets and events. Visible detail/list views poll every five seconds; the reconciler also runs every five seconds. Provider failures invalidate green observations.
5. Start/stop/restart run as owned jobs. An active job locks configuration, secret changes and competing lifecycle operations. Job details expose cancellation and retry from the beginning. Cancellation of a running job retains the application lock until its handler finishes cleanup.

Driver choice is explicit override, `devbox.yaml`, Compose, Dockerfile, then managed runtime detection (image sources choose the image driver). Renaming changes the display name, not the stable slug or managed source path.

## Supported drivers

| Driver | Source authority | Execution and endpoints |
| --- | --- | --- |
| `managed` | Runtime detection or explicit runtime/version/modules | Generated Docker image for PHP, Node.js, Python, Go or static content. One HTTP workload/endpoint. Runtime modules use the existing allowlist. |
| `dockerfile` | Source Dockerfile | Build a single image, publish its HTTP listener. Explicit port changes publishing, not the application code. Set commands in the Dockerfile. |
| `image` | Docker/OCI image | Pull an uncached image before resolving its exposed port. One explicitly selected HTTP/HTTPS/TCP endpoint. Optional `command` and `restart_policy`. |
| `compose` | Source Compose stack | Preserve all services, including database/cache/worker workloads. Exclude infrastructure from automatic HTTP selection. Require explicit selection for multiple equally plausible endpoints. Publish the selected primary endpoint through a DevBox-owned override without editing the source file. |

Automatic port allocation reuses durable endpoint ownership across deployment and process restarts and checks existing legacy port reservations. Explicit occupied ports fail rather than steal another application's port. Single-container replacement retains/restores the previous container on create/start/readiness failure and never releases its active lease. Same-port replacement can interrupt service briefly; this is not zero-downtime deployment.

Compose is **not an atomic rollback engine**. Failed `up`/health checks can leave a partially updated stack. The application inventory retains previous and staged services until successful finalization. Inspect the actual service states and logs before retrying. Named volumes and source files are not rolled back.

Readiness uses container state plus HTTP checks for generated/custom single containers; the image driver supports HTTP(S)/TCP checks. HTTP responses below 500 establish reachability, not business correctness. Compose uses Docker service state/health; no healthcheck in source means application-level health remains unknown. Redirects are not followed by single-container probes.

## Configuration and secrets

`PATCH /api/v1/applications/{id}` accepts name, description, auto_start, desired_state, driver and configuration. `configuration` replaces the stored public configuration; omitted configuration leaves it unchanged. For an application with workloads, a new driver choice is stored as pending while the current driver remains responsible for the running services. The next deployment stops the old workloads to release published ports, then activates the selected driver after a successful deploy. If deployment fails, DevBox attempts to restart the old workloads. Superseded resources are removed through the previous driver; cleanup failures are surfaced as deployment warnings. Compose updates remain nontransactional as described above.

`GET /api/v1/applications/{id}/php-modules` reads `php -m` from the primary running Managed PHP container. The configuration screen compares that inventory with the PHP module catalog and marks each module available (`OK`) or missing (`ERROR`). The endpoint does not execute commands in source-owned Dockerfile, image or Compose containers.

```json
{
  "configuration": {
    "runtime": "php",
    "runtime_version": "8.3",
    "modules": ["gd", "zip", "pdo_mysql"],
    "container_port": 8080,
    "host_port": 0,
    "protocol": "http",
    "health_path": "/",
    "environment": {"APP_ENV": "development"}
  }
}
```

Port zero/omission means automatic selection. `compose_service` selects a Compose service. Unsupported fields, fractional/out-of-range ports and nested password/token values are rejected. Commands belong to the image driver or source files. Public configuration is not a safe place for arbitrary secrets disguised under innocuous keys: use the secret endpoints.

* `GET /applications/{id}/secrets` returns names only.
* `PUT /applications/{id}/secrets/DB_PASSWORD` accepts `{"value":"..."}` and encrypts the value in SecretStore.
* `DELETE /applications/{id}/secrets/DB_PASSWORD` removes the value and binding.

These paths have the `/api/v1` prefix. Secrets are injected into every workload at deploy time, never into the stored deployment plan. Changing a secret requires redeployment. Values are masked in application logs and deployment errors. A host/Docker administrator can still inspect a running container's environment. SecretStore encryption does not protect against a compromised host/master key. Compose secret injection supplies service environment overrides, not `${VARIABLE:?}` interpolation of source Compose files; source-owned `.env`/`env_file` requirements remain the source's responsibility.

## Manifest

`devbox.yaml` supports a deliberately constrained scalar YAML subset with two-space indentation; aliases, tags and inline structures are rejected. A minimal Compose declaration:

```yaml
version: 1
deployment:
  driver: compose
workloads:
  frontend:
    service: web
    role: web
    primary: true
  database:
    service: db
    role: database
endpoints:
  main:
    workload: frontend
    protocol: http
    container_port: 8080
    primary: true
    public: true
environment:
  APP_ENV: development
secrets:
  - DB_PASSWORD
```

Workload aliases only override their referenced Compose services. Missing declared secret names pause deployment for configuration. Multi-workload or multi-endpoint manifests require Compose; single-container drivers reject them instead of silently discarding services.

## API / authorization

Viewer: list/detail/state/workloads/endpoints/deployments/events/logs and secret names. Operator: create/detect/update/deploy/start/stop/restart/reconcile and secret writes. Administrator: remove application and retry a removal job.

* `GET|POST /applications`; `POST /applications/detect`.
* `GET|PATCH|DELETE /applications/{id}`.
* `POST /applications/{id}/{deploy|start|stop|restart|reconcile}`.
* `GET /applications/{id}/{state|workloads|endpoints|deployments|events|logs}`.
* `POST /applications/{id}/jobs/{jobID}/{cancel|retry}` verifies job ownership. Retry records the requesting actor and creates a new job, rebinding deployment history.

All mutation routes retain the core session/CSRF middleware and write audits. Operations return 202 and job IDs; application locks return 409. Job progress is indeterminate when a driver has no numeric measurement; the last recorded stage remains visible.

## Removal and migration boundaries

Migrations 013–015 are additive. They create the application model, job ownership and a unique active-operation index. Existing Project data is preserved, **not automatically adopted**. The old Project lifecycle module is no longer mounted by the composition root, and `/apps` does not call `/projects`. Existing global modules and legacy project-related extension endpoints are not an automatic compatibility layer for new application IDs.

The removal UI requires the application name, removes managed containers/configuration/secrets, and preserves sources, images and persistent volumes. API source deletion is limited to DevBox-owned Git/empty directories. Local source deletion is rejected before external side effects. `remove_volumes` and `remove_generated_images` are explicitly unsupported rather than silently ignored.

This change does not implement automatic conversion of old Project database bindings, domain routes, backups or per-project runtime records to applications. Global database/plugin/Docker operations remain separate. New shared database connectivity can be configured through source/Compose networking and application secret/public environment configuration. Endpoint URLs use the browser's host and observed published port; a stored domain alone does not provision DNS, Nginx or a certificate. There is no automatic domain/TLS-routing integration in this version.

## Verification

Normal checks: `go vet ./...`, `go test ./...`, `go build ./cmd/...` from backend; frontend lint/typecheck/test/build. Application tests exercise SQLite migrations, configuration waiting/recovery, resource identity, cancellation locks, concurrency, secret encryption/redaction, read-only role wiring and manifest/driver precedence. Driver tests cover image metadata pull, old-container restoration and Compose ambiguity/alias handling.

The `Application control plane` workflow runs the race detector and `DEVBOX_TEST_APPLICATION_DOCKER=1 go test ./internal/applications -run '^TestApplicationDockerLifecycle$' -v -count=1 -timeout=10m` on a disposable Docker runner. It uses real image/managed/Dockerfile/Compose drivers, verifies HTTP responses, secret injection, stable redeploy ports, stop/start/restart, local source preservation, Compose volume preservation and foreign-key integrity. It skips unless explicitly enabled and cleans only test-owned resources.

CREATE TABLE applications (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
    slug TEXT NOT NULL COLLATE NOCASE UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    source_type TEXT NOT NULL CHECK(source_type IN ('git','local','docker_image','empty')),
    source_config_json TEXT NOT NULL DEFAULT '{}',
    driver TEXT NOT NULL DEFAULT '',
    desired_state TEXT NOT NULL DEFAULT 'stopped' CHECK(desired_state IN ('running','stopped')),
    observed_state TEXT NOT NULL DEFAULT 'unknown',
    health_state TEXT NOT NULL DEFAULT 'unknown',
    auto_start INTEGER NOT NULL DEFAULT 0 CHECK(auto_start IN (0,1)),
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_applications_state ON applications(desired_state, observed_state);

CREATE TABLE application_sources (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    repository_url TEXT,
    reference TEXT,
    local_path TEXT,
    docker_image TEXT,
    credential_secret_id TEXT,
    current_revision TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE application_runtime (
    application_id TEXT PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    runtime TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    adapter_metadata_json TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL
);

CREATE TABLE workloads (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'internal',
    driver_resource_id TEXT,
    image TEXT,
    desired_state TEXT NOT NULL DEFAULT 'running' CHECK(desired_state IN ('running','stopped')),
    observed_state TEXT NOT NULL DEFAULT 'unknown',
    health_state TEXT NOT NULL DEFAULT 'unknown',
    primary_workload INTEGER NOT NULL DEFAULT 0 CHECK(primary_workload IN (0,1)),
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(application_id, name)
);
CREATE INDEX idx_workloads_application ON workloads(application_id);

CREATE TABLE endpoints (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    workload_id TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    protocol TEXT NOT NULL CHECK(protocol IN ('http','https','tcp')),
    container_port INTEGER NOT NULL CHECK(container_port BETWEEN 1 AND 65535),
    host_port INTEGER CHECK(host_port BETWEEN 1 AND 65535),
    domain TEXT,
    public INTEGER NOT NULL DEFAULT 0 CHECK(public IN (0,1)),
    primary_endpoint INTEGER NOT NULL DEFAULT 0 CHECK(primary_endpoint IN (0,1)),
    tls_mode TEXT NOT NULL DEFAULT 'none',
    health_path TEXT,
    status TEXT NOT NULL DEFAULT 'unknown',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(application_id, name)
);
CREATE INDEX idx_endpoints_application ON endpoints(application_id);
CREATE INDEX idx_endpoints_workload ON endpoints(workload_id);

CREATE TABLE application_environment_variables (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    workload_id TEXT REFERENCES workloads(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(application_id, workload_id, name)
);

CREATE TABLE application_secret_bindings (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    workload_id TEXT REFERENCES workloads(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    secret_scope TEXT NOT NULL,
    secret_name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(application_id, workload_id, name)
);

CREATE TABLE application_volumes (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    workload_id TEXT REFERENCES workloads(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK(type IN ('named_volume','bind_mount','tmpfs')),
    source TEXT NOT NULL DEFAULT '',
    target TEXT NOT NULL,
    read_only INTEGER NOT NULL DEFAULT 0 CHECK(read_only IN (0,1)),
    persistent INTEGER NOT NULL DEFAULT 1 CHECK(persistent IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_application_volumes_application ON application_volumes(application_id);

CREATE TABLE application_deployments (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    job_id TEXT REFERENCES jobs(id) ON DELETE SET NULL,
    driver TEXT NOT NULL,
    status TEXT NOT NULL,
    stage TEXT NOT NULL,
    source_revision TEXT,
    started_at TEXT,
    finished_at TEXT,
    triggered_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    error_text TEXT,
    plan_snapshot_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
CREATE INDEX idx_application_deployments_application ON application_deployments(application_id, created_at DESC);
CREATE INDEX idx_application_deployments_job ON application_deployments(job_id);

CREATE TABLE deployment_workloads (
    deployment_id TEXT NOT NULL REFERENCES application_deployments(id) ON DELETE CASCADE,
    workload_id TEXT NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    resource_id TEXT,
    image TEXT,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY(deployment_id, workload_id)
);

CREATE TABLE application_routes (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    endpoint_id TEXT NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    domain TEXT NOT NULL UNIQUE,
    target_host TEXT NOT NULL DEFAULT '127.0.0.1',
    target_port INTEGER NOT NULL CHECK(target_port BETWEEN 1 AND 65535),
    tls_mode TEXT NOT NULL DEFAULT 'none',
    active INTEGER NOT NULL DEFAULT 0 CHECK(active IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE application_health_checks (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    workload_id TEXT REFERENCES workloads(id) ON DELETE CASCADE,
    endpoint_id TEXT REFERENCES endpoints(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK(type IN ('docker','http','tcp')),
    target TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown',
    message TEXT,
    checked_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX idx_application_health_checks_application ON application_health_checks(application_id);

CREATE TABLE application_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    deployment_id TEXT REFERENCES application_deployments(id) ON DELETE CASCADE,
    workload_id TEXT REFERENCES workloads(id) ON DELETE SET NULL,
    type TEXT NOT NULL,
    stage TEXT,
    message TEXT,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);
CREATE INDEX idx_application_events_application ON application_events(application_id, id DESC);

CREATE TABLE application_port_leases (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    endpoint_id TEXT REFERENCES endpoints(id) ON DELETE SET NULL,
    port INTEGER NOT NULL UNIQUE CHECK(port BETWEEN 1 AND 65535),
    purpose TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'reserved' CHECK(state IN ('reserved','released')),
    created_at TEXT NOT NULL,
    released_at TEXT
);
CREATE INDEX idx_application_port_leases_owner ON application_port_leases(application_id, endpoint_id, state);

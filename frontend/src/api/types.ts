export type Role = 'admin' | 'operator' | 'viewer'

export type OperationalStatus = 'RUNNING' | 'STOPPED' | 'FAILED' | 'BUILDING' | 'DEPLOYING' | 'UNHEALTHY'
export type ProjectSourceType = 'git' | 'local' | 'empty'
export interface CentralCredential {
  id: string
  name: string
  kind: 'token' | 'ssh_key'
  has_secret: boolean
  created_by?: string
  created_at: string
  updated_at: string
}

export interface UpdateStatus {
  current_version: string
  latest_version?: string
  latest_commit_at?: string
  update_available: boolean
  repository: string
  ref: string
  auto_update: boolean
  schedule: string
  last_error?: string
  checked_at: string
}

export interface UpdateProgress {
  state: 'idle' | 'starting' | 'running' | 'succeeded' | 'failed' | 'no_update' | 'unknown'
  percent: number
  stage: string
  message?: string
  current_version?: string
  target_version?: string
  started_at?: string
  updated_at?: string
  finished_at?: string
  error?: string
}

export interface User {
  id: string
  username: string
  role: Role
  active: boolean
  created_at: string
  updated_at: string
}

export interface Job {
  id: string
  type: string
  status: string
  project_id?: string
  requested_by?: string
  payload?: Record<string, unknown>
  result?: Record<string, unknown>
  error?: string
  created_at: string
  started_at?: string
  finished_at?: string
}

export interface AuditEvent {
  id: string
  actor_user_id?: string
  action: string
  resource_type: string
  resource_id?: string
  metadata?: Record<string, unknown>
  remote_addr?: string
  created_at: string
}

export interface SystemInfo {
  hostname: string
  os: string
  arch: string
  go_version: string
  version: string
}

export interface SystemPlatformInfo {
  os: string
  arch: string
  distro_id?: string
  distro_name?: string
  distro_version?: string
  wsl: boolean
  wsl_version?: number
  systemd: boolean
}

export interface SystemComponentStatus {
  name: string
  installed: boolean
  path?: string
  version?: string
  state: string
  error?: string
}

export type RuntimeAvailability = 'available' | 'missing' | 'invalid'

export interface RuntimeDependency {
  name: string
  status: RuntimeAvailability
  version?: string
  path?: string
}

export interface RuntimeInfo {
  runtime: string
  status: RuntimeAvailability
  version?: string
  dependencies?: RuntimeDependency[]
}

export interface RuntimeDetection {
  runtime: string
  framework: string
  confidence: number
  detected_files: string[]
  suggested_build_command?: string
  suggested_start_command?: string
  required_version?: string
  metadata?: Record<string, unknown>
}

export interface ProjectRuntimeInfo {
  project_id: string
  runtime: string
  framework: string
  confidence: number
  detected_files: string[]
  version?: string
  availability: RuntimeAvailability
  dependencies?: RuntimeDependency[]
  build_command?: string
  start_command?: string
  environment?: Record<string, string>
  configured_runtime?: string
}

export interface RuntimeModule {
  name: string
  version?: string
}

export interface RuntimeModuleOption {
  name: string
  label: string
  description: string
  versioned: boolean
}

export interface RuntimeContainerConfig {
  project_id: string
  runtime: string
  runtime_version: string
  container_policy: 'auto' | 'custom'
  modules: RuntimeModule[]
  container_name?: string
  image_tag?: string
  build_fingerprint?: string
}

export interface RuntimeValidation {
  project_id: string
  runtime: string
  availability: RuntimeAvailability
  version?: string
  valid: boolean
  warnings?: string[]
  errors?: string[]
}

export interface DockerStatus {
  available: boolean
  client_version?: string
  server_version?: string
  engine_name?: string
  operating_system?: string
  os_type?: string
  architecture?: string
  docker_root_dir?: string
  cpus?: number
  memory_bytes?: number
  containers?: number
  containers_running?: number
  containers_stopped?: number
  images?: number
  error?: string
}

export interface DockerContainer {
  id: string
  name: string
  image: string
  state: string
  status: string
  ports?: string
  created_at?: string
}

export interface DockerImage {
  id: string
  repository: string
  tag: string
  digest?: string
  size?: string
  created_since?: string
}

export interface DockerVolume {
  name: string
  driver: string
  scope?: string
  mountpoint?: string
}

export interface DockerNetwork {
  id: string
  name: string
  driver: string
  scope?: string
  internal?: string
  ipv6?: string
}

export interface ComposeProject {
  name: string
  config_file: string
}

export interface ComposeProcess {
  name?: string
  service?: string
  state?: string
  health?: string
  image?: string
}

export type DatabaseMode = 'none' | 'managed' | 'compose' | 'external'

export interface DatabaseBinding {
  id?: string
  project_id: string
  mode: DatabaseMode
  database_id?: string
  application_service?: string
  compose_service?: string
  engine?: string
  host?: string
  port?: number
  database?: string
  username?: string
  has_secret: boolean
  host_access_only?: boolean
  application_host?: string
  application_port?: number
  status?: string
  created_at?: string
  updated_at?: string
}

export interface DatabaseBindingInput {
  mode: DatabaseMode
  application_service?: string
  compose_service?: string
  engine?: string
  host?: string
  port?: number
  database?: string
  username?: string
  password?: string
  password_provided?: boolean
  host_access_only?: boolean
}

export interface DatabaseRecord {
  id: string
  project_id?: string
  application_name?: string
  provider: string
  engine: string
  name: string
  status: string
  user?: string
  size_bytes?: number
  created_at: string
  updated_at: string
}

export interface DatabaseUser {
  id: string
  database_id: string
  username: string
  privileges: string[]
  created_at: string
  updated_at: string
}

export interface DatabaseUserCreateResult {
  user: DatabaseUser
  credential: {
    username: string
    password: string
  }
}

export interface DatabaseBackup {
  id: string
  database_id: string
  file_name: string
  status: string
  size_bytes: number
  error?: string
  created_at: string
  completed_at?: string
}

export interface MySQLStatus {
  version?: string
  running: boolean
  connection_state: string
  admin_host?: string
  admin_port?: number
  application_host?: string
  application_port?: number
  network?: string
}

export interface PHPMyAdminStatus {
  installed: boolean
  running: boolean
  state: string
  url: string
}

export interface DockerComposePluginStatus {
  installed: boolean
  mode?: 'plugin' | 'legacy'
  path?: string
  version?: string
  installable: boolean
  message?: string
}

export interface PHPFPMStatus {
  installed: boolean
  path?: string
  version?: string
  installable: boolean
  message?: string
}

export interface MySQLPluginStatus {
  installed: boolean
  running: boolean
  engine?: 'mysql' | 'mariadb'
  path?: string
  server_path?: string
  version?: string
  host?: string
  port?: number
  container_host?: string
  installable: boolean
  message?: string
}

export interface PostgreSQLPluginStatus {
  installed: boolean
  running: boolean
  path?: string
  version?: string
  host?: string
  port?: number
  installable: boolean
  message?: string
}

export interface PHPExtension {
  id: string
  name: string
  description: string
  category: string
  package: string
  installed: boolean
  modules?: string[]
}

export interface ProvisionResult {
  database: DatabaseRecord
  credential: {
    engine: string
    host: string
    port: number
    database: string
    username: string
    password: string
  }
}

export interface PortRecord {
  id: string
  project_id?: string
  application?: string
  port: number
  purpose: string
  state: string
  socket_available: boolean
  created_at: string
  released_at?: string
}

export interface HealthResult {
  status: string
  response_time_ms: number
  error?: string
  checked_at: string
  type?: string
  target?: string
  project_id?: string
}

export interface DomainRecord {
  id: string
  project_id: string
  application?: string
  hostname: string
  target_port: number
  target: string
  tls_enabled: boolean
  status: string
  health?: HealthResult
  created_at: string
  updated_at: string
}

export interface HostChange {
  applied: boolean
  requires_privilege: boolean
  path: string
  instruction?: string
}

export interface DomainMutationResult {
  domain: DomainRecord
  hosts: HostChange
}

export interface ProxyStatus {
  detected: boolean
  version?: string
  config_valid: boolean
  error?: string
}

export interface ProjectSource {
  provider?: string
  repository_url?: string
  reference?: string
}

export interface Project {
  id: string
  name: string
  slug: string
  description: string
  status: string
  source_type: ProjectSourceType
  repository_url?: string
  branch?: string
  local_path: string
  runtime: string
  runtime_version: string
  container_policy: 'auto' | 'custom'
  working_directory: string
  build_command: string
  start_command: string
  healthcheck: string
  auto_start: boolean
  credential_kind?: 'token' | 'ssh_key'
  credential_id?: string
  current_commit?: string
  port?: number
  domain?: string
  open_url?: string
  environment?: Record<string, string>
  source?: ProjectSource
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface DatabaseResource {
  id: string
  name: string
  engine?: string
  status?: string
  project_id?: string
}

export interface PortResource {
  id: string
  port: number
  purpose?: string
  state?: string
  project_id?: string
}

export interface MonitoringMetric {
  available: boolean
  usage_percent: number
}

export interface MonitoringSnapshot {
  collected_at: string
  host_uptime_seconds: number
  cpu: MonitoringMetric & { cores: number }
  memory: MonitoringMetric & {
    total_bytes: number
    used_bytes: number
    free_bytes: number
  }
  disk: MonitoringMetric & {
    path: string
    total_bytes: number
    used_bytes: number
    free_bytes: number
  }
  process: {
    pid: number
    goroutines: number
    heap_allocated_bytes: number
    runtime_reserved_bytes: number
    host_process_count: number
    uptime_seconds: number
  }
  warnings?: string[]
}

export interface LogEntry {
  cursor: number
  id: string
  source: string
  project_id?: string
  job_id?: string
  level: string
  message: string
  fields?: Record<string, unknown>
  created_at: string
}

export interface ServiceProbe {
  name: 'Nginx' | 'MySQL' | 'Docker' | 'DevBox'
  status: string
  message?: string
}

export interface GitCommit {
  hash: string
  author: string
  date: string
  subject: string
}

export interface GitState {
  branch: string
  commit: string
  remote: string
  ahead: number
  behind: number
  dirty: boolean
  branches: string[]
  history: GitCommit[]
}

export interface Deployment {
  id: string
  project_id: string
  job_id?: string
  commit_before?: string
  commit_after?: string
  started_at?: string
  finished_at?: string
  duration_ms: number
  status: string
  stage: string
  error?: string
  triggered_by?: string
  created_at: string
}

export interface ApplicationHealth {
  id: string
  project_id: string
  project_name: string
  type: string
  target: string
  interval_seconds: number
  timeout_seconds: number
  enabled: boolean
  status: string
  message?: string
  response_time_ms: number
  error?: string
  checked_at?: string
}

export interface HealthHistoryEntry {
  id: number
  check_id: string
  project_id: string
  project_name: string
  status: string
  response_time_ms: number
  message?: string
  error?: string
  checked_at: string
}

export interface SystemBackup {
  id: string
  file_name: string
  status: string
  size_bytes: number
  sha256?: string
  requested_by?: string
  error?: string
  created_at: string
  completed_at?: string
}

export interface SystemRestoreRequest {
  backup: SystemBackup
  restart_required: boolean
  message: string
}


export interface ScriptApp {
  id: string
  name: string
  description: string
  install_source: string
  update_source?: string
  uninstall_source?: string
  interpreter: 'bash' | 'sh' | 'pwsh' | 'powershell'
  checksum_sha256?: string
  run_as_root: boolean
  allow_insecure: boolean
  manager: 'script' | 'systemd' | 'docker'
  manager_target?: string
  status: string
  last_error?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export interface ScriptAppCreateResult {
  app: ScriptApp
  job?: Job
}

export interface ScriptAppLogs {
  manager: string
  target?: string
  logs: string
}

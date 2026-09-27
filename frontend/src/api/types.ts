export type Role = 'admin' | 'operator' | 'viewer'

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
}

export interface PHPMyAdminStatus {
  installed: boolean
  running: boolean
  state: string
  url: string
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

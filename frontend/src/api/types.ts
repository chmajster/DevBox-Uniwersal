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

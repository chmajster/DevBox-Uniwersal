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

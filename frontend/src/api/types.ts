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

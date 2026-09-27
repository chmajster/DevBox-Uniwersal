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

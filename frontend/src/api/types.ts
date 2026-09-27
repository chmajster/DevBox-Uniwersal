export type Role = 'admin' | 'operator' | 'viewer'

export type OperationalStatus = 'RUNNING' | 'STOPPED' | 'FAILED' | 'BUILDING' | 'DEPLOYING' | 'UNHEALTHY'

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

export interface ProjectSource {
  provider?: string
  repository_url?: string
  reference?: string
}

export interface Project {
  id: string
  name: string
  slug?: string
  description?: string
  status: string
  source_type?: string
  repository_url?: string
  branch?: string
  local_path?: string
  runtime?: string
  deployment_mode?: string
  working_directory?: string
  build_command?: string
  start_command?: string
  healthcheck?: string
  auto_start?: boolean
  open_url?: string
  environment?: Record<string, string>
  source?: ProjectSource
  created_at?: string
  updated_at?: string
}

export interface DockerContainer {
  id: string
  name: string
  state: string
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

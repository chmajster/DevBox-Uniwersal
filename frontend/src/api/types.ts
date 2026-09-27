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

export type ProjectSourceType = 'git' | 'local' | 'empty'
export type DeploymentMode = 'native' | 'docker'

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
  deployment_mode: DeploymentMode
  working_directory: string
  build_command: string
  start_command: string
  healthcheck: string
  auto_start: boolean
  credential_kind?: 'token' | 'ssh_key'
  current_commit?: string
  port?: number
  domain?: string
  created_at: string
  updated_at: string
  archived_at?: string
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

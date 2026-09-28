import { request } from './client'
import type {
  ApplicationHealth,
  DatabaseResource,
  DockerContainer,
  DockerStatus,
  HealthHistoryEntry,
  Job,
  LogEntry,
  MonitoringSnapshot,
  MySQLStatus,
  PortResource,
  Project,
  ProxyStatus,
  ServiceProbe,
  SystemBackup,
  SystemRestoreRequest
} from './types'

type CollectionPayload<T> =
  | T[]
  | {
      items?: T[]
      projects?: T[]
      containers?: T[]
      databases?: T[]
      ports?: T[]
    }

function asCollection<T>(payload: CollectionPayload<T> | null | undefined): T[] {
  if (!payload) return []
  if (Array.isArray(payload)) return payload
  return payload.items ?? payload.projects ?? payload.containers ?? payload.databases ?? payload.ports ?? []
}

export async function listProjects() {
  return asCollection(await request<CollectionPayload<Project>>('/projects'))
}

export function getProject(projectID: string) {
  return request<Project>(`/projects/${encodeURIComponent(projectID)}`)
}

export function runProjectAction(projectID: string, action: 'start' | 'stop' | 'restart' | 'deploy') {
  return request<Job>(`/projects/${encodeURIComponent(projectID)}/actions/${action}`, {
    method: 'POST',
    body: JSON.stringify({})
  })
}

export function getProjectTool(projectID: string, tool: 'open' | 'terminal') {
  return request<{ url: string }>(`/projects/${encodeURIComponent(projectID)}/${tool}`, {
    method: tool === 'terminal' ? 'POST' : 'GET',
    body: tool === 'terminal' ? JSON.stringify({}) : undefined
  })
}

export async function listDockerContainers() {
  return asCollection(await request<CollectionPayload<DockerContainer>>('/docker/containers'))
}

export async function listDatabases(projectID?: string) {
  const suffix = projectID ? `?project_id=${encodeURIComponent(projectID)}` : ''
  return asCollection(await request<CollectionPayload<DatabaseResource>>(`/databases${suffix}`))
}

export async function listPorts(projectID?: string) {
  const suffix = projectID ? `?project_id=${encodeURIComponent(projectID)}` : ''
  return asCollection(await request<CollectionPayload<PortResource>>(`/ports${suffix}`))
}

export function getMonitoringSnapshot() {
  return request<MonitoringSnapshot>('/monitoring/snapshot')
}

export async function getServiceProbe(name: ServiceProbe['name']): Promise<ServiceProbe> {
  if (name === 'DevBox') {
    await request<unknown>('/health')
    return { name, status: 'RUNNING' }
  }

  if (name === 'Docker') {
    const result = await request<DockerStatus>('/docker/status')
    return {
      name,
      status: result.available ? 'RUNNING' : 'UNHEALTHY',
      message: result.error ?? result.server_version ?? result.client_version
    }
  }

  if (name === 'MySQL') {
    const result = await request<MySQLStatus>('/mysql/status')
    return {
      name,
      status: result.running ? 'RUNNING' : 'UNHEALTHY',
      message: result.running ? result.version : result.connection_state
    }
  }

  const result = await request<ProxyStatus>('/proxy/status')
  return {
    name,
    status: result.detected && result.config_valid ? 'RUNNING' : 'UNHEALTHY',
    message: result.error ?? result.version
  }
}

export function listLogSources() {
  return request<string[]>('/logs/sources')
}

export interface LogFilters {
  source: string
  project?: string
  level?: string
  search?: string
  after?: number
  limit?: number
  since?: string
  until?: string
}

export function logQuery(filters: LogFilters) {
  const params = new URLSearchParams()
  params.set('source', filters.source)
  if (filters.project) params.set('project', filters.project)
  if (filters.level) params.set('level', filters.level)
  if (filters.search) params.set('search', filters.search)
  if (filters.after) params.set('after', String(filters.after))
  if (filters.limit) params.set('limit', String(filters.limit))
  if (filters.since) params.set('since', filters.since)
  if (filters.until) params.set('until', filters.until)
  return params.toString()
}

export function listLogs(filters: LogFilters) {
  return request<LogEntry[]>(`/logs?${logQuery(filters)}`)
}

export function listJobLogs(jobID: string) {
  return request<LogEntry[]>(`/jobs/${encodeURIComponent(jobID)}/logs`)
}

export function listHealthChecks() {
  return request<ApplicationHealth[]>('/health-checks')
}

export function listHealthHistory(projectID?: string, limit = 200) {
  const params = new URLSearchParams()
  if (projectID) params.set('project_id', projectID)
  params.set('limit', String(limit))
  return request<HealthHistoryEntry[]>(`/health-checks/history?${params.toString()}`)
}

export function runProjectHealthCheck(projectID: string) {
  return request<ApplicationHealth>(`/projects/${encodeURIComponent(projectID)}/health-check`, {
    method: 'POST',
    body: JSON.stringify({})
  })
}

export function listSystemBackups() {
  return request<SystemBackup[]>('/system-backups')
}

export function createSystemBackup() {
  return request<SystemBackup>('/system-backups', {
    method: 'POST',
    body: JSON.stringify({})
  })
}

export function importSystemBackup(file: File) {
  const body = new FormData()
  body.append('file', file)
  return request<SystemBackup>('/system-backups/import', {
    method: 'POST',
    body
  })
}

export function restoreSystemBackup(id: string) {
  return request<SystemRestoreRequest>(`/system-backups/${encodeURIComponent(id)}/restore`, {
    method: 'POST',
    body: JSON.stringify({})
  })
}

export function deleteSystemBackup(id: string) {
  return request<{ deleted: boolean; id: string }>(`/system-backups/${encodeURIComponent(id)}`, {
    method: 'DELETE'
  })
}

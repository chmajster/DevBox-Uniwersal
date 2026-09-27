import { request } from './client'
import type {
  DatabaseResource,
  DockerContainer,
  Job,
  LogEntry,
  MonitoringSnapshot,
  PortResource,
  Project,
  ServiceProbe
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
  return asCollection(await request<CollectionPayload<PortResource>>(`/networking/ports${suffix}`))
}

export function getMonitoringSnapshot() {
  return request<MonitoringSnapshot>('/monitoring/snapshot')
}

export async function getServiceProbe(name: ServiceProbe['name']): Promise<ServiceProbe> {
  if (name === 'DevBox') {
    await request<unknown>('/health')
    return { name, status: 'RUNNING' }
  }
  const endpoint = name === 'Docker' ? '/docker/status' : name === 'MySQL' ? '/databases/status' : '/proxy/status'
  const result = await request<{ status?: string; message?: string }>(endpoint)
  return {
    name,
    status: result.status ?? 'UNHEALTHY',
    message: result.message
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
}

export function logQuery(filters: LogFilters) {
  const params = new URLSearchParams()
  params.set('source', filters.source)
  if (filters.project) params.set('project', filters.project)
  if (filters.level) params.set('level', filters.level)
  if (filters.search) params.set('search', filters.search)
  if (filters.after) params.set('after', String(filters.after))
  if (filters.limit) params.set('limit', String(filters.limit))
  return params.toString()
}

export function listLogs(filters: LogFilters) {
  return request<LogEntry[]>(`/logs?${logQuery(filters)}`)
}

export function listJobLogs(jobID: string) {
  return request<LogEntry[]>(`/jobs/${encodeURIComponent(jobID)}/logs`)
}

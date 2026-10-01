import type { Application } from '../applications/model'
import { request } from '../api/client'
import type { DatabaseResource, DockerContainer, DockerStatus, Job, MySQLStatus, PortResource, ProxyStatus, SystemInfo } from '../api/types'

export interface ServiceReading { name: string; state: 'running' | 'unhealthy' | 'unknown'; detail: string; elapsed: number | null; to: string }
export interface Overview {
  applications: Application[] | null; containers: DockerContainer[] | null; databases: DatabaseResource[] | null
  ports: PortResource[] | null; jobs: Job[] | null; system: SystemInfo | null
  services: ServiceReading[]; errors: string[]
}
export function collection<T>(value: T[] | Record<string, T[]> | null): T[] {
  if (value === null) return []
  if (Array.isArray(value)) return value
  for (const key of ['items', 'projects', 'containers', 'databases', 'ports', 'jobs']) if (Array.isArray(value[key])) return value[key]
  throw new Error('API zwróciło nieprawidłową kolekcję.')
}
async function list<T>(path: string, signal: AbortSignal) { return collection(await request<T[] | Record<string, T[]> | null>(path, { signal })) }
async function services(signal: AbortSignal): Promise<ServiceReading[]> {
  const readers = [
    { name: 'DevBox (API)', to: '/health', load: async () => { await request('/health', { signal }); return { ok: true, detail: 'Panel zarządzania' } } },
    { name: 'Docker', to: '/docker', load: async () => { const r = await request<DockerStatus>('/docker/status', { signal }); return { ok: r.available, detail: r.error || r.server_version || 'Docker Engine' } } },
    { name: 'Nginx', to: '/domains', load: async () => { const r = await request<ProxyStatus>('/proxy/status', { signal }); return { ok: r.detected && r.config_valid, detail: r.error || r.version || 'Konfiguracja reverse proxy' } } },
    { name: 'MySQL', to: '/databases', load: async () => { const r = await request<MySQLStatus>('/mysql/status', { signal }); return { ok: r.running, detail: r.version || r.connection_state || 'Baza danych' } } }
  ]
  return Promise.all(readers.map(async (reader): Promise<ServiceReading> => {
    const start = performance.now()
    try { const result = await reader.load(); return { name: reader.name, to: reader.to, state: result.ok ? 'running' : 'unhealthy', detail: result.detail, elapsed: Math.round(performance.now() - start) } }
    catch (reason) { return { name: reader.name, to: reader.to, state: 'unknown', detail: reason instanceof Error ? reason.message : 'Brak odczytu API', elapsed: null } }
  }))
}
export async function loadOverview(signal: AbortSignal): Promise<Overview> {
  const results = await Promise.allSettled([list<Application>('/applications', signal), list<DockerContainer>('/docker/containers', signal), list<DatabaseResource>('/databases', signal), list<PortResource>('/ports', signal), list<Job>('/jobs', signal), request<SystemInfo>('/system/info', { signal }), services(signal)] as const)
  const errors: string[] = []
  function value<T>(result: PromiseSettledResult<T>, name: string): T | null {
    if (result.status === 'fulfilled') return result.value
    errors.push(`${name}: ${result.reason instanceof Error ? result.reason.message : 'Brak danych'}`)
    return null
  }
  return { applications: value(results[0], 'Aplikacje'), containers: value(results[1], 'Kontenery'), databases: value(results[2], 'Bazy danych'), ports: value(results[3], 'Porty'), jobs: value(results[4], 'Zadania'), system: value(results[5], 'System'), services: value(results[6], 'Usługi') ?? [], errors }
}

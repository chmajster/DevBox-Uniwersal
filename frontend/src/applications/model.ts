import { encodeEnvironment, type EnvironmentRow } from './environment'
export type Configuration = Record<string, unknown>
export interface Endpoint {
  id: string; name: string; workload_id: string; protocol: string; container_port: number
  host_port?: number; domain?: string; primary: boolean; public: boolean; status: string; route_active?: boolean; tls_mode?: string
}
export interface Workload {
  id: string; name: string; role: string; image?: string; primary: boolean
  driver_resource_id?: string; observed_state: string; health_state: string
}
export interface Deployment {
  id: string; job_id?: string; status: string; stage: string; driver: string; error?: string
  created_at: string; source_revision?: string; finished_at?: string; plan_snapshot?: { runtime?: { metadata?: { profile?: string } } }
}
export interface Application {
  id: string; name: string; slug: string; description: string; source_type: string; driver: string
  source: { repository_url?: string; reference?: string; local_path?: string; credential_id?: string; docker_image?: string }
  source_config?: Configuration; desired_state: string; observed_state: string; health_state: string
  auto_start: boolean; status: string; created_at: string; updated_at: string
  runtime?: { name: string; version?: string; metadata?: Record<string, unknown> }; primary_endpoint?: Endpoint; workload_count?: number
  last_deployment?: Deployment; active_operation?: { id: string; type: string; status: string }
}
export interface ApplicationDetail extends Application { workloads: Workload[]; endpoints: Endpoint[]; deployments: Deployment[] }
export interface Detection {
  driver: string; profile?: string; runtime?: string; version?: string; confidence: string; requires_configuration: boolean
  warnings?: string[]; reasons?: string[]; start_command?: string; compose_found?: boolean; dockerfile_found?: boolean
  services?: { name: string; suggested_role: string; primary: boolean; reason?: string }[]
  endpoints?: { service: string; protocol: string; container_port: number; primary: boolean; reason?: string }[]
}
export interface CreateApplication {
  name: string; description: string; source_type: string; source: Application['source']
  auto_start: boolean; configuration: Configuration
}
export const sourceNames: Record<string, string> = { git: 'Repozytorium Git', local: 'Katalog lokalny', empty: 'Pusta aplikacja', docker_image: 'Obraz OCI' }
export const statusNames: Record<string, string> = {
  running: 'RUNNING', stopped: 'STOPPED', starting: 'STARTING', restarting: 'RESTARTING', building: 'BUILDING', unknown: 'UNKNOWN',
  degraded: 'Częściowo sprawna', failed: 'FAILED', missing: 'Brak kontenera', exited: 'Zakończona',
  healthy: 'Sprawna', unhealthy: 'Niesprawna', queued: 'W kolejce', success: 'Sukces',
  cancelled: 'Anulowana', waiting_for_configuration: 'Wymaga konfiguracji'
}
export function endpointURL(endpoint: Endpoint | undefined, hostname: string): string | null {
  // Only a real published HTTP(S) mapping becomes a browser link. Configured
  // domains do not imply that a DNS record or reverse-proxy route exists.
  if (endpoint?.domain && endpoint.route_active && endpoint.status === 'running') {
    if (!/^[a-z0-9.-]+$/i.test(endpoint.domain)) return null
    return `${endpoint.tls_mode === 'existing' ? 'https' : 'http'}://${endpoint.domain}/`
  }
  if (!endpoint?.host_port || endpoint.status !== 'running' || !['http', 'https'].includes(endpoint.protocol)) return null
  if (!Number.isInteger(endpoint.host_port) || endpoint.host_port < 1 || endpoint.host_port > 65535) return null
  const host = hostname.startsWith('[') ? hostname : hostname.includes(':') ? `[${hostname}]` : hostname
  try {
    const url = new URL(`${endpoint.protocol}://${host}:${endpoint.host_port}`)
    if (url.username || url.password || url.hostname === '') return null
    return url.toString()
  } catch { return null }
}
export function readConfiguration(form: FormData): Configuration {
  const config: Configuration = {}
  for (const key of ['container_port', 'host_port']) {
    const text = String(form.get(key) ?? '').trim()
    if (!text) continue
    const value = Number(text)
    if (!Number.isInteger(value) || value < 0 || value > 65535) throw new Error('Port musi być liczbą całkowitą od 0 do 65535.')
    config[key] = value
  }
  for (const key of ['deployment_mode', 'runtime', 'runtime_version', 'compose_service', 'protocol', 'health_path', 'start_command', 'domain', 'tls_mode', 'working_directory', 'mount_target', 'restart_policy', 'document_root']) {
    const value = String(form.get(key) ?? '').trim()
    if (value) config[key] = value
  }
  const modules = String(form.get('modules') ?? '').split(/[\s,]+/).filter(Boolean)
  if (modules.length) config.modules = modules
  if (form.has('environment_rows')) {
    const rows = JSON.parse(String(form.get('environment_rows'))) as EnvironmentRow[]
    const { publicValues } = encodeEnvironment(rows)
    if (Object.keys(publicValues).length) config.environment = publicValues
  }
  const env: Record<string, string> = {}
  for (const line of String(form.get('environment') ?? '').split('\n')) {
    if (!line.trim()) continue
    const position = line.indexOf('=')
    const name = line.slice(0, position).trim()
    if (position < 1 || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) throw new Error('Zmienne podaj jako NAZWA=wartość, po jednej na linię.')
    env[name] = line.slice(position + 1)
  }
  if (Object.keys(env).length) config.environment = env
  return config
}
export function validateManagedConfiguration(config: Configuration) {
  if (config.deployment_mode !== 'auto') return
  if (!['php', 'node', 'python', 'go', 'static'].includes(String(config.runtime ?? ''))) throw new Error('Wybierz język / oprogramowanie kontenera.')
  if (!config.runtime_version) throw new Error('Wybierz wersję oprogramowania kontenera.')
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(String(config.runtime_version))) throw new Error('Podaj poprawną wersję, np. 8.3 lub 8.3.12, bez spacji.')
}
export function message(error: unknown) { return error instanceof Error ? error.message : String(error) }
export function deploymentModeName(mode: unknown, driver = '') {
  if (mode === 'dockerfile') return 'Dockerfile aplikacji'
  if (mode === 'image') return 'Obraz OCI'
  if (mode === 'compose' || (!mode && driver === 'compose')) return 'Docker Compose z aplikacji'
  if (!mode && driver && !['managed', 'compose'].includes(driver)) return 'Zachowana konfiguracja aplikacji'
  if (mode === 'auto' || !mode) return 'DevBox Managed Runtime'
  return 'DevBox Managed Runtime'
}
export function date(value?: string) { return value ? new Date(value).toLocaleString('pl-PL') : '—' }

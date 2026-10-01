export type Configuration = Record<string, unknown>
export interface Endpoint {
  id: string; name: string; workload_id: string; protocol: string; container_port: number
  host_port?: number; domain?: string; primary: boolean; public: boolean; status: string
}
export interface Workload {
  id: string; name: string; role: string; image?: string; primary: boolean
  driver_resource_id?: string; observed_state: string; health_state: string
}
export interface Deployment {
  id: string; job_id?: string; status: string; stage: string; driver: string; error?: string
  created_at: string; source_revision?: string; finished_at?: string
}
export interface Application {
  id: string; name: string; slug: string; description: string; source_type: string; driver: string
  source: { repository_url?: string; reference?: string; local_path?: string; docker_image?: string; credential_id?: string }
  source_config?: Configuration; desired_state: string; observed_state: string; health_state: string
  auto_start: boolean; status: string; created_at: string; updated_at: string
  runtime?: { name: string; version?: string }; primary_endpoint?: Endpoint; workload_count?: number
  last_deployment?: Deployment; active_operation?: { id: string; type: string; status: string }
}
export interface ApplicationDetail extends Application { workloads: Workload[]; endpoints: Endpoint[]; deployments: Deployment[] }
export interface Detection {
  driver: string; runtime?: string; version?: string; confidence: string; requires_configuration: boolean
  warnings?: string[]; reasons?: string[]
  services?: { name: string; suggested_role: string; primary: boolean; reason?: string }[]
  endpoints?: { service: string; protocol: string; container_port: number; primary: boolean; reason?: string }[]
}
export interface CreateApplication {
  name: string; description: string; source_type: string; source: Application['source']
  driver?: string; auto_start: boolean; configuration: Configuration
}
export const sourceNames: Record<string, string> = { git: 'Repozytorium Git', local: 'Katalog lokalny', docker_image: 'Obraz Docker / OCI', empty: 'Pusta aplikacja' }
export const statusNames: Record<string, string> = {
  running: 'Uruchomiona', stopped: 'Zatrzymana', starting: 'Uruchamianie', unknown: 'Brak odczytu',
  degraded: 'Częściowo sprawna', failed: 'Błąd', missing: 'Brak kontenera', exited: 'Zakończona',
  healthy: 'Sprawna', unhealthy: 'Niesprawna', queued: 'W kolejce', success: 'Sukces',
  cancelled: 'Anulowana', waiting_for_configuration: 'Wymaga konfiguracji'
}
export function endpointURL(endpoint: Endpoint | undefined, hostname: string): string | null {
  // Only a real published HTTP(S) mapping becomes a browser link. Configured
  // domains do not imply that a DNS record or reverse-proxy route exists.
  if (!endpoint?.host_port || !['http', 'https'].includes(endpoint.protocol)) return null
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
  for (const key of ['runtime', 'runtime_version', 'compose_service', 'protocol', 'health_path']) {
    const value = String(form.get(key) ?? '').trim()
    if (value) config[key] = value
  }
  const modules = String(form.get('modules') ?? '').split(/[\s,]+/).filter(Boolean)
  if (modules.length) config.modules = modules
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
export function message(error: unknown) { return error instanceof Error ? error.message : String(error) }
export function date(value?: string) { return value ? new Date(value).toLocaleString('pl-PL') : '—' }

export interface PortSettings {
  container_port: number
  host_port: number
  https_enabled: boolean
  https_container_port: number
  https_host_port: number
  compose_service: string
}

export interface PublishedPort {
  host_port: number
  container_port: number
}

export interface PortConfiguration {
  settings: PortSettings
  configured: boolean
  applied?: {
    settings: PortSettings
    http: PublishedPort
    https?: PublishedPort
    applied_at: string
  }
}

export const defaultPortSettings: PortSettings = {
  container_port: 0,
  host_port: 8080,
  https_enabled: false,
  https_container_port: 443,
  https_host_port: 8443,
  compose_service: '',
}

export function validatePortSettings(settings: PortSettings): string | null {
  const port = (value: number) => Number.isInteger(value) && value >= 1 && value <= 65535
  if (settings.container_port !== 0 && !port(settings.container_port)) return 'Port kontenera musi być liczbą całkowitą od 1 do 65535. Puste pole oznacza automatyczne wykrywanie.'
  if (!port(settings.host_port) || !port(settings.https_host_port) || !port(settings.https_container_port)) return 'Porty muszą być liczbami całkowitymi od 1 do 65535.'
  if (settings.https_enabled && settings.container_port !== 0 && settings.container_port === settings.https_container_port) return 'HTTP i HTTPS muszą wskazywać różne porty wewnętrzne kontenera.'
  if (settings.compose_service && !/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/.test(settings.compose_service)) return 'Nieprawidłowa nazwa usługi Compose.'
  return null
}

export function samePortSettings(a: PortSettings, b: PortSettings): boolean {
  return a.container_port === b.container_port && a.host_port === b.host_port &&
    a.https_enabled === b.https_enabled && a.https_container_port === b.https_container_port &&
    a.https_host_port === b.https_host_port && a.compose_service === b.compose_service
}

export function publishedApplicationURL(base: string, port: number, https: boolean): string {
  const url = new URL(base)
  url.protocol = https ? 'https:' : 'http:'
  url.port = String(port)
  url.pathname = '/'
  url.search = ''
  url.hash = ''
  url.username = ''
  url.password = ''
  return url.toString()
}

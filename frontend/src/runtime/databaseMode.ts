import type { DatabaseMode } from '../api/types'

export const dockerHostDatabaseHost = 'host.docker.internal'
export type DatabaseModeChoice = DatabaseMode | 'host'

export function isDockerHostDatabaseHost(host?: string): boolean {
  const normalized = (host ?? '').trim().toLowerCase()
  return normalized === dockerHostDatabaseHost ||
    normalized === 'localhost' ||
    normalized === '127.0.0.1' ||
    normalized === '::1' ||
    normalized === '[::1]'
}

export function databaseModeChoice(mode: DatabaseMode, hostAccessOnly = false): DatabaseModeChoice {
  if (mode === 'external' && hostAccessOnly) return 'host'
  return mode
}

export type DatabaseModeField =
  | 'application_service'
  | 'compose_service'
  | 'engine'
  | 'host'
  | 'port'
  | 'database'
  | 'username'
  | 'password'
  | 'application_host'
  | 'application_port'
  | 'status'
  | 'created_at'

const fields: Record<DatabaseMode, DatabaseModeField[]> = {
  none: [],
  managed: ['application_service', 'engine', 'database', 'username', 'application_host', 'application_port', 'status', 'created_at'],
  compose: ['application_service', 'compose_service', 'port', 'database', 'username', 'password', 'application_host', 'application_port'],
  external: ['application_service', 'host', 'port', 'database', 'username', 'password', 'application_host', 'application_port'],
}

export function databaseModeFields(mode: DatabaseMode): readonly DatabaseModeField[] {
  return fields[mode]
}

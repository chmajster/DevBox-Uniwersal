import type { DatabaseMode } from '../api/types'

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

const fields: Record<DatabaseMode, DatabaseModeField[]> = {
  none: [],
  managed: ['application_service', 'engine', 'database', 'username', 'application_host', 'application_port'],
  compose: ['application_service', 'compose_service', 'port', 'database', 'username', 'password', 'application_host', 'application_port'],
  external: ['host', 'port', 'database', 'username', 'password', 'application_host', 'application_port'],
}

export function databaseModeFields(mode: DatabaseMode): readonly DatabaseModeField[] {
  return fields[mode]
}

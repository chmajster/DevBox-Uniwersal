import type { DatabaseMode } from '../api/types'

export type DatabaseModeField =
  | 'application_service'
  | 'compose_service'
  | 'host'
  | 'port'
  | 'database'
  | 'username'
  | 'password'
  | 'application_host'
  | 'application_port'
  | 'status'

const fields: Record<DatabaseMode, DatabaseModeField[]> = {
  none: [],
  managed: ['application_service', 'database', 'username', 'application_host', 'application_port', 'status'],
  compose: ['application_service', 'compose_service', 'port', 'database', 'username', 'password', 'application_host', 'application_port'],
  external: ['application_service', 'host', 'port', 'database', 'username', 'password', 'application_host', 'application_port'],
}

export function databaseModeFields(mode: DatabaseMode): readonly DatabaseModeField[] {
  return fields[mode]
}

import type { OperationalStatus } from './api/types'

const aliases: Record<string, OperationalStatus> = {
  active: 'RUNNING',
  healthy: 'RUNNING',
  ok: 'RUNNING',
  run: 'RUNNING',
  running: 'RUNNING',
  started: 'RUNNING',
  succeeded: 'RUNNING',
  inactive: 'STOPPED',
  stopped: 'STOPPED',
  cancelled: 'STOPPED',
  draft: 'STOPPED',
  error: 'FAILED',
  failed: 'FAILED',
  building: 'BUILDING',
  build: 'BUILDING',
  pending: 'BUILDING',
  queued: 'DEPLOYING',
  deploy: 'DEPLOYING',
  deploying: 'DEPLOYING',
  provisioning: 'DEPLOYING',
  unhealthy: 'UNHEALTHY',
  unavailable: 'UNHEALTHY'
}

export function normalizeOperationalStatus(value?: string): OperationalStatus {
  if (!value) return 'UNHEALTHY'
  return aliases[value.trim().toLowerCase()] ?? 'UNHEALTHY'
}

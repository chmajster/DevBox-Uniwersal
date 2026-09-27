import type { OperationalStatus } from './api/types'

const aliases: Record<string, OperationalStatus> = {
  active: 'RUNNING',
  healthy: 'RUNNING',
  ok: 'RUNNING',
  running: 'RUNNING',
  started: 'RUNNING',
  succeeded: 'RUNNING',
  inactive: 'STOPPED',
  stopped: 'STOPPED',
  cancelled: 'STOPPED',
  error: 'FAILED',
  failed: 'FAILED',
  building: 'BUILDING',
  build: 'BUILDING',
  queued: 'DEPLOYING',
  deploy: 'DEPLOYING',
  deploying: 'DEPLOYING',
  provisioning: 'DEPLOYING',
  unhealthy: 'UNHEALTHY',
  unavailable: 'UNHEALTHY'
}

export function normalizeOperationalStatus(value?: string): OperationalStatus {
  if (!value) return 'UNHEALTHY'
  const normalized = value.trim().toLowerCase()
  return aliases[normalized] ?? (normalized === 'run' ? 'RUNNING' : 'UNHEALTHY')
}

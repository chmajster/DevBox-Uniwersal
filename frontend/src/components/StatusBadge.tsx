import type { OperationalStatus } from '../api/types'
import { normalizeOperationalStatus } from '../status'

const icons: Record<OperationalStatus, string> = {
  RUNNING: '●',
  STOPPED: '■',
  FAILED: '×',
  BUILDING: '◆',
  DEPLOYING: '↻',
  UNHEALTHY: '!'
}

export function StatusBadge({ status }: { status: string }) {
  const normalized = normalizeOperationalStatus(status)
  return (
    <span className={`status-badge status-${normalized.toLowerCase()}`} data-status={normalized}>
      <span aria-hidden="true">{icons[normalized]}</span>
      <span>{normalized}</span>
    </span>
  )
}

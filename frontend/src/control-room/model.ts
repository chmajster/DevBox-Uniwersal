import type { Job, MonitoringSnapshot } from '../api/types'

export type MetricName = 'cpu' | 'memory' | 'disk'
export interface Sample { at: number; cpu: number | null; memory: number | null; disk: number | null }
export const HISTORY_WINDOW = 2 * 60 * 60 * 1000
export const HISTORY_LIMIT = 721
export function percent(value: number | undefined): number | null {
  return value !== undefined && Number.isFinite(value) ? Math.min(100, Math.max(0, value)) : null
}
export function appendSample(history: Sample[], snapshot: MonitoringSnapshot): Sample[] {
  const at = Date.parse(snapshot.collected_at)
  if (!Number.isFinite(at) || (history.length > 0 && at <= history[history.length - 1].at)) return history
  const sample: Sample = { at, cpu: snapshot.cpu.available ? percent(snapshot.cpu.usage_percent) : null,
    memory: snapshot.memory.available ? percent(snapshot.memory.usage_percent) : null,
    disk: snapshot.disk.available ? percent(snapshot.disk.usage_percent) : null }
  return [...history.filter((item) => item.at >= at - HISTORY_WINDOW), sample].slice(-HISTORY_LIMIT)
}
/** Never connect gaps (hidden tab, disconnected API, unavailable metric). */
export function chartSegments(samples: Sample[], metric: MetricName): { at: number; value: number }[][] {
  const result: { at: number; value: number }[][] = []
  let segment: { at: number; value: number }[] = []
  for (const sample of samples) {
    const value = sample[metric]
    if (value === null || (segment.length > 0 && sample.at - segment[segment.length - 1].at > 30000)) {
      if (segment.length) result.push(segment)
      segment = []
    }
    if (value !== null) segment.push({ at: sample.at, value })
  }
  if (segment.length) result.push(segment)
  return result
}
export function bytes(value?: number): string {
  if (value === undefined || !Number.isFinite(value) || value < 0) return '—'
  if (value === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.max(0, Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1))
  return `${(value / 1024 ** index).toFixed(index >= 3 ? 1 : 0)} ${units[index]}`
}
export function uptime(seconds?: number): string {
  if (seconds === undefined || !Number.isFinite(seconds) || seconds < 0) return '—'
  const days = Math.floor(seconds / 86400), hours = Math.floor(seconds % 86400 / 3600)
  return days ? `${days} dni ${hours} godz.` : `${hours} godz. ${Math.floor(seconds % 3600 / 60)} min`
}
export function time(value: string | number | undefined, date = false): string {
  if (value === undefined) return '—'
  const instant = new Date(value)
  if (!Number.isFinite(instant.getTime())) return '—'
  return date ? instant.toLocaleString('pl-PL') : instant.toLocaleTimeString('pl-PL', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
export function recent<T extends { created_at: string }>(items: T[]): T[] {
  return [...items].sort((a, b) => (Date.parse(b.created_at) || 0) - (Date.parse(a.created_at) || 0))
}
export function jobState(status: string): { label: string; tone: string } {
  switch (status.toLowerCase()) {
    case 'queued': case 'pending': return { label: 'OCZEKUJE', tone: 'warning' }
    case 'running': return { label: 'W TOKU', tone: 'info' }
    case 'succeeded': case 'completed': return { label: 'ZAKOŃCZONE', tone: 'success' }
    case 'failed': return { label: 'BŁĄD', tone: 'danger' }
    case 'cancelled': case 'canceled': return { label: 'ANULOWANE', tone: 'muted' }
    default: return { label: status || 'NIEZNANY', tone: 'muted' }
  }
}
export function pendingJobs(jobs: Job[]): Job[] { return jobs.filter((job) => ['queued', 'pending', 'running'].includes(job.status.toLowerCase())) }

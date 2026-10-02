import type { Job, LogEntry } from '../api/types'

function numberValue(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function stringValue(value: unknown) {
  return typeof value === 'string' && value.trim() ? value : undefined
}

export function jobProgress(job: Job) {
  const explicit = numberValue(job.result?.progress) ?? numberValue(job.payload?.progress)
  if (explicit !== undefined) return Math.max(0, Math.min(100, explicit))
  if (job.status === 'succeeded') return 100
  return 0
}

export function jobStage(job: Job) {
  return stringValue(job.result?.stage) ?? stringValue(job.payload?.stage) ?? job.type
}

export function elapsedSeconds(job: Job, now = new Date()) {
  const start = new Date(job.started_at ?? job.created_at).getTime()
  const end = job.finished_at ? new Date(job.finished_at).getTime() : now.getTime()
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return 0
  return Math.floor((end - start) / 1000)
}

export function formatDuration(seconds: number) {
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remaining = seconds % 60
  if (hours > 0) return `${hours}h ${minutes}m ${remaining}s`
  if (minutes > 0) return `${minutes}m ${remaining}s`
  return `${remaining}s`
}

export function JobProgress({ job, logs, now }: { job: Job; logs: LogEntry[]; now?: Date }) {
  const progress = jobProgress(job)
  const lastStage = [...logs].reverse().find((entry) => typeof entry.fields?.stage === 'string')?.fields?.stage
  const stage = typeof lastStage === 'string' ? lastStage : jobStage(job)
  const indeterminate = ['running', 'queued'].includes(job.status) && numberValue(job.result?.progress) === undefined && numberValue(job.payload?.progress) === undefined
  return (
    <div className="job-progress">
      <div className="job-progress-grid">
        <div><span>Stage</span><strong>{stage}</strong></div>
        <div><span>Progress</span><strong>{indeterminate ? 'W toku' : `${Math.round(progress)}%`}</strong></div>
        <div><span>Elapsed</span><strong>{formatDuration(elapsedSeconds(job, now))}</strong></div>
      </div>
      <progress max={100} value={indeterminate ? undefined : progress}>{Math.round(progress)}%</progress>
      {job.error && <div className="error-banner" role="alert">{job.error}</div>}
      <div className="log-console" aria-label="Job logs">
        {logs.length === 0
          ? <div className="muted">Brak wpisów w logu zadania.</div>
          : logs.map((entry) => (
              <div className="log-line job-log-line" key={entry.id}>
                <time>{new Date(entry.created_at).toLocaleTimeString()}</time>
                <strong>{entry.level.toUpperCase()}</strong>
                <span>{entry.message}{typeof entry.fields?.stage === 'string' ? ` · ${entry.fields.stage}` : ''}</span>
              </div>
            ))}
      </div>
    </div>
  )
}

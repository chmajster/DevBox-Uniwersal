import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { Job, LogEntry } from '../api/types'

export function isTerminalJob(status: string) {
  return ['succeeded', 'failed', 'cancelled'].includes(status)
}

export function readPendingJob(key: string): string {
  try { return localStorage.getItem(key) ?? '' } catch { return '' }
}
export function storePendingJob(key: string, id: string) {
  try { if (id) localStorage.setItem(key, id); else localStorage.removeItem(key) } catch { /* Storage may be disabled. */ }
}

interface Props { jobId: string; onComplete?: (job: Job) => void }
export function DurableJobNotice({ jobId, onComplete }: Props) {
  const [job, setJob] = useState<Job | null>(null)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [error, setError] = useState('')
  const complete = useRef(onComplete)
  useEffect(() => { complete.current = onComplete }, [onComplete])
  useEffect(() => {
    if (!jobId) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    let delivered = false
    setJob(null)
    setLogs([])
    setError('')
    const poll = async () => {
      try {
        const current = await request<Job>(`/jobs/${encodeURIComponent(jobId)}`, { signal: controller.signal })
        const entries = await request<LogEntry[]>(`/jobs/${encodeURIComponent(jobId)}/logs`, { signal: controller.signal }).catch(() => [])
        if (controller.signal.aborted) return
        setJob(current)
        setLogs(entries.slice(-10))
        setError('')
        if (isTerminalJob(current.status)) {
          if (!delivered) { delivered = true; complete.current?.(current) }
          return
        }
      } catch (cause) {
        if (controller.signal.aborted) return
        setError(cause instanceof Error ? cause.message : 'Odczyt zadania nie powiódł się. Ponawianie…')
      }
      timer = setTimeout(() => { void poll() }, 1200)
    }
    void poll()
    return () => { controller.abort(); if (timer) clearTimeout(timer) }
  }, [jobId])
  if (!jobId) return null
  return <section className="validation-box" aria-live="polite" aria-label="Stan zadania">
    <div className="actions"><strong>Zadanie: {job?.status ?? 'odczytywanie'}</strong><Link to={`/jobs?job=${encodeURIComponent(jobId)}`}>Pełne logi i szczegóły</Link></div>
    {job?.error && <p className="error-banner">{job.error}</p>}
    {error && <p className="error-banner">{error}</p>}
    {logs.length > 0 && <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '16rem', overflow: 'auto' }}>{logs.map((entry) => entry.message).join('\n')}</pre>}
  </section>
}

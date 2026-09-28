import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { apiURL, request } from '../api/client'
import { listJobLogs } from '../api/operations'
import type { Job, LogEntry } from '../api/types'
import { ErrorState } from '../components/ErrorState'
import { JobProgress } from '../components/JobProgress'
import { collection } from '../control-room/api'
import { jobState, pendingJobs, recent, time } from '../control-room/model'
import { usePolling } from '../control-room/usePolling'

async function loadJobs(signal: AbortSignal) { return collection(await request<Job[] | null>('/jobs', { signal })) }
export function JobsPage() {
  const [params, setParams] = useSearchParams()
  const selected = params.get('job') ?? ''
  const onlyActive = params.get('status') === 'active'
  const listing = usePolling(loadJobs, 3000)
  const jobs = listing.data ?? []
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [error, setError] = useState('')
  const selectedJob = jobs.find((job) => job.id === selected)
  const selectedStatus = selectedJob?.status
  useEffect(() => {
    let cancelled = false
    setLogs([]); setError('')
    if (!selected) return
    listJobLogs(selected).then((items) => { if (!cancelled) setLogs(items ?? []) }).catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason)) })
    if (selectedStatus !== 'running' && selectedStatus !== 'queued') return () => { cancelled = true }
    const stream = new EventSource(apiURL(`/jobs/${encodeURIComponent(selected)}/logs/stream`), { withCredentials: true })
    const onLog = (event: MessageEvent<string>) => {
      try { const entry = JSON.parse(event.data) as LogEntry; setLogs((current) => [...current.filter((item) => item.id !== entry.id), entry].slice(-500)) }
      catch { setError('Nieprawidłowy wpis ze strumienia logów zadania.') }
    }
    stream.addEventListener('log', onLog as EventListener)
    stream.addEventListener('error', () => setError('Strumień logów zadania został rozłączony.'))
    return () => { cancelled = true; stream.close() }
  }, [selected, selectedStatus])
  function inspect(id: string) { const next = new URLSearchParams(params); if (id === selected) next.delete('job'); else next.set('job', id); setParams(next) }
  const visible = recent(onlyActive ? pendingJobs(jobs) : jobs)
  return <>
    <div className="page-heading"><div><h1>Zadania</h1><p className="muted">Kolejka operacji, postęp oraz trwałe logi wykonania.</p></div><label><span className="sr-only">Filtr zadań</span><select value={onlyActive ? 'active' : 'all'} onChange={(event) => { const next = new URLSearchParams(params); if (event.target.value === 'active') next.set('status', 'active'); else next.delete('status'); setParams(next) }}><option value="all">Wszystkie zadania</option><option value="active">Oczekujące i w toku</option></select></label></div>
    {(error || listing.error) && <ErrorState message={error || listing.error} />}
    <div className="table-wrap"><table><caption className="sr-only">Zadania operacyjne</caption><thead><tr><th>Typ</th><th>Status</th><th>Utworzono</th><th>Szczegóły</th></tr></thead><tbody>
      {visible.map((job) => { const state = jobState(job.status); return <tr key={job.id}><td>{job.type}</td><td><span className={`console-badge badge-${state.tone}`}>{state.label}</span></td><td>{time(job.created_at, true)}</td><td><button className="secondary-button" onClick={() => inspect(job.id)} aria-expanded={selected === job.id}>{selected === job.id ? 'Ukryj' : 'Sprawdź'}</button></td></tr> })}
      {visible.length === 0 && <tr><td colSpan={4} className="muted">{listing.loading ? 'Pobieranie zadań…' : listing.error ? 'Zadania niedostępne.' : 'Brak zadań dla wybranego filtra.'}</td></tr>}
    </tbody></table></div>
    {selectedJob && <section className="job-details"><h2>{selectedJob.type}</h2><JobProgress job={selectedJob} logs={logs} /></section>}
  </>
}

import { useEffect, useState } from 'react'
import { apiURL, request } from '../api/client'
import { listJobLogs } from '../api/operations'
import type { Job, LogEntry } from '../api/types'
import { ErrorState } from '../components/ErrorState'
import { JobProgress } from '../components/JobProgress'

export function JobsPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [selected, setSelected] = useState<string>('')
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const items = await request<Job[]>('/jobs')
        if (!cancelled) setJobs(items ?? [])
      } catch (reason) {
        if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason))
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), 3000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  const selectedJob = jobs.find((job) => job.id === selected)
  const selectedStatus = selectedJob?.status

  useEffect(() => {
    if (!selected) {
      setLogs([])
      return
    }
    let cancelled = false
    listJobLogs(selected)
      .then((items) => { if (!cancelled) setLogs(items) })
      .catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason)) })

    if (selectedStatus !== 'running' && selectedStatus !== 'queued') {
      return () => { cancelled = true }
    }

    const stream = new EventSource(apiURL(`/jobs/${encodeURIComponent(selected)}/logs/stream`), { withCredentials: true })
    const onLog = (event: MessageEvent<string>) => {
      try {
        const entry = JSON.parse(event.data) as LogEntry
        setLogs((current) => [...current.filter((item) => item.id !== entry.id), entry].slice(-500))
      } catch {
        setError('Job log stream returned an invalid event payload')
      }
    }
    stream.addEventListener('log', onLog as EventListener)
    stream.addEventListener('error', () => setError(`Job log stream disconnected for ${selected}`))
    return () => {
      cancelled = true
      stream.close()
    }
  }, [selected, selectedStatus])

  return <>
    <div className="page-heading"><div><h1>Jobs</h1><p className="muted">Stage, progress, elapsed time and durable job logs.</p></div></div>
    {error && <ErrorState message={error} />}
    <div className="table-wrap">
      <table>
        <thead><tr><th>Type</th><th>Status</th><th>Created</th><th>Details</th></tr></thead>
        <tbody>
          {jobs.map((job) => <tr key={job.id}>
            <td>{job.type}</td>
            <td>{job.status}</td>
            <td>{new Date(job.created_at).toLocaleString()}</td>
            <td><button className="secondary-button" type="button" onClick={() => setSelected((current) => current === job.id ? '' : job.id)}>
              {selected === job.id ? 'Hide' : 'Inspect'}
            </button></td>
          </tr>)}
          {jobs.length === 0 && <tr><td colSpan={4} className="muted">No jobs yet.</td></tr>}
        </tbody>
      </table>
    </div>
    {selectedJob && <section className="job-details"><h2>{selectedJob.type}</h2><JobProgress job={selectedJob} logs={logs} /></section>}
  </>
}

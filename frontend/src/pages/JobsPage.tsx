import { useEffect, useState } from 'react'
import { request } from '../api/client'
import type { Job } from '../api/types'

export function JobsPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [error, setError] = useState('')
  useEffect(() => { request<Job[]>('/jobs').then((items) => setJobs(items ?? [])).catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load jobs')) }, [])
  return <><h1>Jobs</h1>{error && <div className="error-banner">{error}</div>}
    <table><thead><tr><th>Type</th><th>Status</th><th>Created</th></tr></thead><tbody>
      {jobs.map((job) => <tr key={job.id}><td>{job.type}</td><td>{job.status}</td><td>{new Date(job.created_at).toLocaleString()}</td></tr>)}
      {jobs.length === 0 && <tr><td colSpan={3} className="muted">No jobs yet.</td></tr>}
    </tbody></table></>
}

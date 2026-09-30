import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { listProjects } from '../api/operations'
import type { Project } from '../api/types'
import { ErrorState } from '../components/ErrorState'
import { StatusBadge } from '../components/StatusBadge'

export function ApplicationsPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    listProjects()
      .then((items) => { if (!cancelled) setProjects(items) })
      .catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { cancelled = true }
  }, [])

  return <>
    <div className="page-heading">
      <div>
        <h1>Applications</h1>
        <p className="muted">Projects managed through the project/runtime/deployment APIs.</p>
      </div>
    </div>
    {error && <ErrorState message={error} />}
    <div className="table-wrap">
      <table>
        <thead><tr><th>Name</th><th>Runtime</th><th>Container</th><th>Status</th><th>Path</th></tr></thead>
        <tbody>
          {projects.map((project) => <tr key={project.id}>
            <td><Link to={`/projects/${encodeURIComponent(project.id)}/overview`}>{project.name}</Link></td>
            <td>{project.runtime ?? '—'}</td>
            <td>{project.container_policy === 'custom' ? 'Custom Docker' : 'Managed Docker'}</td>
            <td><StatusBadge status={project.status} /></td>
            <td>{project.working_directory ?? project.local_path ?? '—'}</td>
          </tr>)}
          {projects.length === 0 && !error && <tr><td colSpan={5} className="muted">No applications.</td></tr>}
        </tbody>
      </table>
    </div>
  </>
}

import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { listDockerContainers, listProjects } from '../api/operations'
import type { DockerContainer, Project } from '../api/types'
import { ErrorState } from '../components/ErrorState'
import { StatusBadge } from '../components/StatusBadge'
import { resolveApplicationRuntimeStatus } from './applicationRuntimeStatus'

const LIVE_STATUS_REFRESH_MS = 10_000

export function ApplicationsPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [containers, setContainers] = useState<DockerContainer[]>([])
  const [dockerStateAvailable, setDockerStateAvailable] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false

    async function refresh() {
      const [projectResult, containerResult] = await Promise.allSettled([
        listProjects(),
        listDockerContainers(),
      ])
      if (cancelled) return

      if (projectResult.status === 'fulfilled') {
        setProjects(projectResult.value)
        setError('')
      } else {
        const reason = projectResult.reason
        setError(reason instanceof Error ? reason.message : String(reason))
      }

      if (containerResult.status === 'fulfilled') {
        setContainers(containerResult.value)
        setDockerStateAvailable(true)
      } else {
        setContainers([])
        setDockerStateAvailable(false)
      }
    }

    void refresh()
    const interval = window.setInterval(() => { void refresh() }, LIVE_STATUS_REFRESH_MS)
    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
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
          {projects.map((project) => {
            const status = resolveApplicationRuntimeStatus(project, containers, dockerStateAvailable)
            return <tr key={project.id}>
              <td><Link to={`/projects/${encodeURIComponent(project.id)}/overview`}>{project.name}</Link></td>
              <td>{project.runtime ?? '—'}</td>
              <td>{project.container_policy === 'custom' ? 'Custom Docker' : 'Managed Docker'}</td>
              <td><StatusBadge status={status} /></td>
              <td>{project.working_directory ?? project.local_path ?? '—'}</td>
            </tr>
          })}
          {projects.length === 0 && !error && <tr><td colSpan={5} className="muted">No applications.</td></tr>}
        </tbody>
      </table>
    </div>
  </>
}

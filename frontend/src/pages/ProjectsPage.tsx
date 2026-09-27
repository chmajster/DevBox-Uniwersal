import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { Job, Project } from '../api/types'
import { useAuth } from '../auth/AuthContext'

export function ProjectsPage() {
  const { user } = useAuth()
  const [projects, setProjects] = useState<Project[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const load = () => request<Project[]>('/projects').then((items) => setProjects(items ?? [])).catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load applications'))
  useEffect(() => { void load() }, [])
  async function deploy(project: Project) { setBusy(project.id); setError(''); try { await request<Job>(`/projects/${project.id}/deploy`, { method: 'POST' }); await load() } catch (cause) { setError(cause instanceof Error ? cause.message : 'Deployment failed to queue') } finally { setBusy('') } }
  async function archive(project: Project) { setBusy(project.id); setError(''); try { await request<Project>(`/projects/${project.id}/archive`, { method: 'POST' }); await load() } catch (cause) { setError(cause instanceof Error ? cause.message : 'Archive failed') } finally { setBusy('') } }
  return <>
    <div className="page-heading"><div><h1>Aplikacje</h1><p className="muted">Projekty lokalne, Git i deploymenty.</p></div>{user?.role !== 'viewer' && <Link className="button-link" to="/apps/new">Dodaj aplikację</Link>}</div>
    {error && <div className="error-banner">{error}</div>}
    <div className="table-wrap"><table><thead><tr><th>Name</th><th>Status</th><th>Runtime</th><th>Branch</th><th>Port</th><th>Domain</th><th>Commit</th><th>Actions</th></tr></thead><tbody>
      {projects.map((project) => <tr key={project.id}><td><Link to={`/apps/${project.id}`}>{project.name}</Link></td><td><span className="status-pill">{project.status}</span></td><td>{project.runtime || '—'}</td><td>{project.branch || '—'}</td><td>{project.port ?? '—'}</td><td>{project.domain ?? '—'}</td><td><code>{project.current_commit?.slice(0, 10) || '—'}</code></td><td><div className="actions"><Link to={`/apps/${project.id}`}>Szczegóły</Link>{user?.role !== 'viewer' && <><button type="button" className="button-compact" disabled={busy === project.id} onClick={() => void deploy(project)}>Deploy</button><button type="button" className="button-compact secondary" disabled={busy === project.id} onClick={() => void archive(project)}>Archiwizuj</button></>}</div></td></tr>)}
      {projects.length === 0 && <tr><td colSpan={8} className="muted">Brak aplikacji.</td></tr>}
    </tbody></table></div>
  </>
}

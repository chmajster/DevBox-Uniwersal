import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { request } from '../api/client'
import type { Deployment, GitState, Job, Project } from '../api/types'
import { useAuth } from '../auth/AuthContext'

type Tab = 'overview' | 'git' | 'deployments' | 'configuration'

export function ProjectDetailPage() {
  const { id = '' } = useParams()
  const { user } = useAuth()
  const [project, setProject] = useState<Project | null>(null)
  const [git, setGit] = useState<GitState | null>(null)
  const [deployments, setDeployments] = useState<Deployment[]>([])
  const [tab, setTab] = useState<Tab>('overview')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [config, setConfig] = useState({ runtime: '', deployment_mode: 'native', working_directory: '', build_command: '', start_command: '', healthcheck: '', auto_start: false })

  async function loadProject() {
    const item = await request<Project>(`/projects/${id}`)
    setProject(item)
    setConfig({ runtime: item.runtime, deployment_mode: item.deployment_mode, working_directory: item.working_directory, build_command: item.build_command, start_command: item.start_command, healthcheck: item.healthcheck, auto_start: item.auto_start })
  }
  async function loadDeployments() {
    setDeployments((await request<Deployment[]>(`/projects/${id}/deployments`)) ?? [])
  }
  async function loadGit() {
    try {
      setGit(await request<GitState>(`/projects/${id}/git`))
    } catch (cause) {
      setGit(null)
      setError(cause instanceof Error ? cause.message : 'Git state unavailable')
    }
  }

  useEffect(() => {
    loadProject().catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load application'))
    void loadDeployments()
  }, [id])
  useEffect(() => {
    if (tab === 'git') void loadGit()
    if (tab === 'deployments') void loadDeployments()
  }, [tab])

  async function queue(path: string, name: string, body?: unknown) {
    setBusy(name)
    setError('')
    try {
      await request<Job>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) })
      await loadProject()
      await loadDeployments()
      if (tab === 'git') await loadGit()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : `${name} failed`)
    } finally {
      setBusy('')
    }
  }

  async function save(event: FormEvent) {
    event.preventDefault()
    setBusy('save')
    setError('')
    try {
      const updated = await request<Project>(`/projects/${id}`, { method: 'PATCH', body: JSON.stringify(config) })
      setProject(updated)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Save failed')
    } finally {
      setBusy('')
    }
  }

  if (!project) return <>{error ? <div className="error-banner">{error}</div> : <p className="muted">Loading…</p>}</>

  return <>
    <div className="page-heading"><div><Link to="/apps" className="muted-link">← Aplikacje</Link><h1>{project.name}</h1><p className="muted">{project.description || project.local_path}</p></div>{user?.role !== 'viewer' && <button type="button" disabled={busy !== ''} onClick={() => void queue(`/projects/${id}/deploy`, 'deploy')}>Deploy</button>}</div>
    {error && <div className="error-banner">{error}</div>}
    <div className="tabs">{(['overview', 'git', 'deployments', 'configuration'] as Tab[]).map((item) => <button key={item} type="button" className={tab === item ? 'active' : ''} onClick={() => { setError(''); setTab(item) }}>{item === 'overview' ? 'Overview' : item === 'git' ? 'Git' : item === 'deployments' ? 'Deployments' : 'Configuration'}</button>)}</div>
    {tab === 'overview' && <div className="summary-grid panel"><div><span>Status</span><strong>{project.status}</strong></div><div><span>Source</span><strong>{project.source_type}</strong></div><div><span>Runtime</span><strong>{project.runtime || '—'}</strong></div><div><span>Mode</span><strong>{project.deployment_mode}</strong></div><div><span>Branch</span><strong>{project.branch || '—'}</strong></div><div><span>Commit</span><strong><code>{project.current_commit?.slice(0, 12) || '—'}</code></strong></div><div><span>Port</span><strong>{project.port ?? '—'}</strong></div><div><span>Domain</span><strong>{project.domain ?? '—'}</strong></div><div className="span-2"><span>Local path</span><strong><code>{project.local_path}</code></strong></div></div>}
    {tab === 'git' && <div className="stack">
      {user?.role !== 'viewer' && <div className="toolbar"><button type="button" className="secondary" disabled={busy !== ''} onClick={() => void queue(`/projects/${id}/git/fetch`, 'fetch')}>Fetch</button><button type="button" disabled={busy !== ''} onClick={() => void queue(`/projects/${id}/git/pull`, 'pull')}>Pull --ff-only</button></div>}
      {git && <><div className="summary-grid panel"><div><span>Branch</span><strong>{git.branch}</strong></div><div><span>Commit</span><strong><code>{git.commit.slice(0, 12)}</code></strong></div><div><span>Ahead / behind</span><strong>{git.ahead} / {git.behind}</strong></div><div><span>Working tree</span><strong>{git.dirty ? 'dirty' : 'clean'}</strong></div><div className="span-2"><span>Remote</span><strong>{git.remote || '—'}</strong></div></div>
      <div className="panel"><h2>Branches</h2><div className="branch-list">{git.branches.map((branch) => <span key={branch}>{branch}</span>)}</div></div>
      <div className="table-wrap"><table><thead><tr><th>Commit</th><th>Author</th><th>Date</th><th>Subject</th></tr></thead><tbody>{git.history.map((commit) => <tr key={commit.hash}><td><code>{commit.hash.slice(0, 10)}</code></td><td>{commit.author}</td><td>{new Date(commit.date).toLocaleString()}</td><td>{commit.subject}</td></tr>)}</tbody></table></div></>}
    </div>}
    {tab === 'deployments' && <div className="table-wrap"><table><thead><tr><th>Status</th><th>Stage</th><th>Commit before</th><th>Commit after</th><th>Started</th><th>Duration</th><th>Error</th></tr></thead><tbody>{deployments.map((item) => <tr key={item.id}><td>{item.status}</td><td>{item.stage}</td><td><code>{item.commit_before?.slice(0, 10) || '—'}</code></td><td><code>{item.commit_after?.slice(0, 10) || '—'}</code></td><td>{item.started_at ? new Date(item.started_at).toLocaleString() : '—'}</td><td>{item.duration_ms ? `${item.duration_ms} ms` : '—'}</td><td className="error-cell">{item.error || '—'}</td></tr>)}{deployments.length === 0 && <tr><td colSpan={7} className="muted">Brak deploymentów.</td></tr>}</tbody></table></div>}
    {tab === 'configuration' && <form className="panel form-grid" onSubmit={save}>
      <label>Runtime<input disabled={user?.role === 'viewer'} value={config.runtime} onChange={(e) => setConfig({ ...config, runtime: e.target.value })} /></label>
      <label>Deployment mode<select disabled={user?.role === 'viewer'} value={config.deployment_mode} onChange={(e) => setConfig({ ...config, deployment_mode: e.target.value })}><option value="native">native</option><option value="docker">docker</option></select></label>
      <label className="span-2">Working directory<input disabled={user?.role === 'viewer'} value={config.working_directory} onChange={(e) => setConfig({ ...config, working_directory: e.target.value })} /></label>
      <label className="span-2">Build command<input disabled={user?.role === 'viewer'} value={config.build_command} onChange={(e) => setConfig({ ...config, build_command: e.target.value })} /></label>
      <label className="span-2">Start command<input disabled={user?.role === 'viewer'} value={config.start_command} onChange={(e) => setConfig({ ...config, start_command: e.target.value })} /></label>
      <label className="span-2">Healthcheck<input disabled={user?.role === 'viewer'} value={config.healthcheck} onChange={(e) => setConfig({ ...config, healthcheck: e.target.value })} /></label>
      <label className="checkbox"><input disabled={user?.role === 'viewer'} type="checkbox" checked={config.auto_start} onChange={(e) => setConfig({ ...config, auto_start: e.target.checked })} /> Auto start</label>
      {user?.role !== 'viewer' && <div className="span-2"><button type="submit" disabled={busy !== ''}>{busy === 'save' ? 'Zapisywanie…' : 'Zapisz konfigurację'}</button></div>}
    </form>}
  </>
}

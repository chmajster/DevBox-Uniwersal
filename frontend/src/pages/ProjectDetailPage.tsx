import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { apiURL, request } from '../api/client'
import { listLogs, logQuery } from '../api/operations'
import type { Deployment, GitState, Job, LogEntry, Project } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { ProjectRuntimeSection } from '../runtime/ProjectRuntimeSection'
import { ProjectDatabaseSection } from '../runtime/ProjectDatabaseSection'
import { ProjectPortsSection } from '../runtime/ProjectPortsSection'
import { publishedApplicationURL } from '../runtime/portSettings'

type Tab = 'overview' | 'git' | 'deployments' | 'logs' | 'runtime' | 'database' | 'ports' | 'settings'

const projectDetailTabs: Array<{ id: Tab; label: string }> = [
  { id: 'overview', label: 'Overview' },
  { id: 'git', label: 'Git' },
  { id: 'deployments', label: 'Deployments' },
  { id: 'logs', label: 'Logi' },
  { id: 'runtime', label: 'Runtime' },
  { id: 'database', label: 'Baza danych' },
  { id: 'ports', label: 'Porty' },
  { id: 'settings', label: 'Ustawienia' },
]

function tabFromParam(value: string | null): Tab {
  return projectDetailTabs.some((item) => item.id === value) ? value as Tab : 'overview'
}

const deploymentStages = ['QUEUED', 'PREPARING', 'UPDATING_SOURCE', 'DATABASE', 'DEPENDENCIES', 'BUILDING', 'STARTING', 'HEALTHCHECK', 'SUCCESS'] as const

const deploymentStageLabels: Record<string, string> = {
  QUEUED: 'W kolejce',
  PREPARING: 'Przygotowanie',
  UPDATING_SOURCE: 'Aktualizacja źródła',
  DATABASE: 'Konfiguracja bazy danych',
  DEPENDENCIES: 'Instalacja zależności',
  BUILDING: 'Budowanie obrazu',
  STARTING: 'Uruchamianie kontenera',
  HEALTHCHECK: 'Sprawdzanie healthcheck',
  SUCCESS: 'Zakończono',
  FAILED: 'Błąd',
}

function deploymentFinished(item: Deployment) {
  return item.status === 'SUCCESS' || item.status === 'FAILED' || item.stage === 'SUCCESS' || item.stage === 'FAILED'
}

function deploymentProgress(item: Deployment) {
  if (item.status === 'SUCCESS' || item.stage === 'SUCCESS') return 100
  const index = deploymentStages.indexOf(item.stage as (typeof deploymentStages)[number])
  if (index < 0) return 0
  return Math.round((index / (deploymentStages.length - 1)) * 100)
}


export function ProjectDetailPage() {
  const { id = '' } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const { user } = useAuth()
  const [project, setProject] = useState<Project | null>(null)
  const [git, setGit] = useState<GitState | null>(null)
  const [gitError, setGitError] = useState('')
  const [gitLoading, setGitLoading] = useState(false)
  const [deployments, setDeployments] = useState<Deployment[]>([])
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [logSource, setLogSource] = useState('all')
  const [logLevel, setLogLevel] = useState('')
  const [logSearch, setLogSearch] = useState('')
  const [liveLogs, setLiveLogs] = useState(true)
  const logCursorRef = useRef(0)
  const [tab, setTab] = useState<Tab>(() => tabFromParam(searchParams.get('tab')))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [config, setConfig] = useState({ working_directory: '', build_command: '', start_command: '', healthcheck: '', auto_start: false })

  async function loadProject() {
    const item = await request<Project>(`/projects/${id}`)
    setProject(item)
    setConfig({ working_directory: item.working_directory, build_command: item.build_command, start_command: item.start_command, healthcheck: item.healthcheck, auto_start: item.auto_start })
  }
  async function loadDeployments() {
    const items = (await request<Deployment[]>(`/projects/${id}/deployments`)) ?? []
    setDeployments(items)
    return items
  }
  async function loadGit() {
    setGitLoading(true)
    setGitError('')
    try {
      setGit(await request<GitState>(`/projects/${id}/git`))
    } catch (cause) {
      setGit(null)
      setGitError(cause instanceof Error ? cause.message : 'Git state unavailable')
    } finally {
      setGitLoading(false)
    }
  }

  async function loadLogs() {
    try {
      const entries = await listLogs({
        source: logSource,
        project: id,
        level: logLevel || undefined,
        search: logSearch || undefined,
        limit: 300,
      })
      setLogs(entries ?? [])
      logCursorRef.current = entries?.length ? entries[entries.length - 1].cursor : 0
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać logów aplikacji')
    }
  }

  useEffect(() => {
    loadProject().catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load application'))
    void loadDeployments()
  }, [id])
  useEffect(() => {
    if (tab === 'git') void loadGit()
    if (tab === 'deployments') void loadDeployments()
    if (tab === 'logs') {
      logCursorRef.current = 0
      setLogs([])
      void loadLogs()
    }
  }, [tab, id])

  useEffect(() => {
    if (tab !== 'logs') return
    logCursorRef.current = 0
    setLogs([])
    void loadLogs()
  }, [logSource, logLevel])

  useEffect(() => {
    if (tab !== 'logs' || !liveLogs) return
    const query = logQuery({
      source: logSource,
      project: id,
      level: logLevel || undefined,
      search: logSearch || undefined,
      after: logCursorRef.current || undefined,
      limit: 300,
    })
    const stream = new EventSource(apiURL(`/logs/stream?${query}`), { withCredentials: true })
    const onLog = (event: MessageEvent<string>) => {
      try {
        const entry = JSON.parse(event.data) as LogEntry
        logCursorRef.current = Math.max(logCursorRef.current, entry.cursor)
        setLogs((current) => [...current.filter((item) => item.id !== entry.id), entry].slice(-500))
      } catch {
        setError('Strumień logów zwrócił nieprawidłowy wpis.')
      }
    }
    stream.addEventListener('log', onLog as EventListener)
    stream.addEventListener('error', () => setError('Połączenie ze strumieniem logów zostało przerwane.'))
    return () => stream.close()
  }, [tab, liveLogs, logSource, logLevel, logSearch, id])

  const activeDeployment = deployments.find((item) => !deploymentFinished(item))
  const currentDeployment = activeDeployment ?? deployments[0]

  function selectTab(next: Tab) {
    setError('')
    setTab(next)
    const params = new URLSearchParams(searchParams)
    if (next === 'overview') params.delete('tab')
    else params.set('tab', next)
    setSearchParams(params, { replace: true })
  }

  useEffect(() => {
    if (!activeDeployment) return
    let cancelled = false
    let timer = 0

    const poll = async () => {
      try {
        const items = await loadDeployments()
        if (cancelled) return
        const tracked = items.find((item) => item.id === activeDeployment.id)
        if (tracked && deploymentFinished(tracked)) {
          await loadProject()
          return
        }
      } catch (cause) {
        if (!cancelled) setError(cause instanceof Error ? cause.message : 'Nie udało się odświeżyć stanu deploymentu')
      }
      if (!cancelled) timer = window.setTimeout(() => void poll(), 1000)
    }

    timer = window.setTimeout(() => void poll(), 1000)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [activeDeployment?.id, id])

  async function deploy() {
    setBusy('deploy')
    setError('')
    selectTab('deployments')
    try {
      await request<Job>(`/projects/${id}/deploy`, { method: 'POST' })
      await loadProject()
      await loadDeployments()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Deploy failed')
    } finally {
      setBusy('')
    }
  }

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

  const applicationURL = project.port
    ? publishedApplicationURL(typeof window === 'undefined' ? 'http://localhost/' : window.location.href, project.port, false)
    : ''

  return <>
    <div className="page-heading"><div><Link to="/apps" className="muted-link">← Aplikacje</Link><h1>{project.name}</h1><p className="muted">{project.description || project.local_path}</p></div>{user?.role !== 'viewer' && <button type="button" disabled={busy !== '' || Boolean(activeDeployment)} onClick={() => void deploy()}>{busy === 'deploy' ? 'Uruchamianie…' : activeDeployment ? `Deploy: ${deploymentStageLabels[activeDeployment.stage] ?? activeDeployment.stage}` : 'Deploy'}</button>}</div>
    {error && <div className="error-banner">{error}</div>}
    <nav className="tabs project-detail-tabs" aria-label="Sekcje aplikacji" role="tablist">
      {projectDetailTabs.map((item) => <button
        key={item.id}
        type="button"
        role="tab"
        aria-selected={tab === item.id}
        className={tab === item.id ? 'active' : ''}
        onClick={() => selectTab(item.id)}
      >{item.label}</button>)}
    </nav>
    {tab === 'overview' && <div className="stack">
      <div className="summary-grid panel"><div><span>Status</span><strong>{project.status}</strong></div><div><span>Source</span><strong>{project.source_type}</strong></div><div><span>Runtime</span><strong>{project.runtime || 'auto-detect'}{project.runtime_version ? ` ${project.runtime_version}` : ''}</strong></div><div><span>Kontener</span><strong>{project.container_policy === 'custom' ? 'własny Docker' : 'automatyczny'}</strong></div><div><span>Branch</span><strong>{project.branch || '—'}</strong></div><div><span>Commit</span><strong><code>{project.current_commit?.slice(0, 12) || '—'}</code></strong></div><div><span>Port</span><strong>{project.port ?? '—'}</strong></div><div><span>Domain</span><strong>{project.domain ?? '—'}</strong></div><div className="span-2"><span>Adres aplikacji</span><strong>{applicationURL ? <a href={applicationURL} target="_blank" rel="noopener noreferrer" aria-label={`Otwórz aplikację ${project.name} w nowej karcie`}>{applicationURL}</a> : '—'}</strong></div><div className="span-2"><span>Local path</span><strong><code>{project.local_path}</code></strong></div></div>
    </div>}
    {tab === 'git' && <div className="stack">
      {gitLoading && <div className="panel"><p className="muted">Sprawdzanie repozytorium Git…</p></div>}
      {!gitLoading && gitError && <div className="panel">
        <h2>Git niedostępny dla tego projektu</h2>
        <p className="muted">
          {project.source_type === 'local'
            ? 'Projekt został dodany z lokalnego katalogu. Operacje Fetch/Pull są dostępne tylko wtedy, gdy ten katalog jest repozytorium Git i może zostać odczytany przez DevBox.'
            : 'Nie udało się odczytać stanu repozytorium Git.'}
        </p>
        <p className="muted small"><code>{gitError}</code></p>
      </div>}
      {git && user?.role !== 'viewer' && git.remote && <div className="toolbar"><button type="button" className="secondary" disabled={busy !== ''} onClick={() => void queue(`/projects/${id}/git/fetch`, 'fetch')}>Fetch</button><button type="button" disabled={busy !== ''} onClick={() => void queue(`/projects/${id}/git/pull`, 'pull')}>Pull --ff-only</button></div>}
      {git && <><div className="summary-grid panel"><div><span>Branch</span><strong>{git.branch}</strong></div><div><span>Commit</span><strong><code>{git.commit.slice(0, 12)}</code></strong></div><div><span>Ahead / behind</span><strong>{git.ahead} / {git.behind}</strong></div><div><span>Working tree</span><strong>{git.dirty ? 'dirty' : 'clean'}</strong></div><div className="span-2"><span>Remote</span><strong>{git.remote || '—'}</strong></div></div>
      <div className="panel"><h2>Branches</h2><div className="branch-list">{git.branches.map((branch) => <span key={branch}>{branch}</span>)}</div></div>
      <div className="table-wrap"><table><thead><tr><th>Commit</th><th>Author</th><th>Date</th><th>Subject</th></tr></thead><tbody>{git.history.map((commit) => <tr key={commit.hash}><td><code>{commit.hash.slice(0, 10)}</code></td><td>{commit.author}</td><td>{new Date(commit.date).toLocaleString()}</td><td>{commit.subject}</td></tr>)}</tbody></table></div></>}
    </div>}
    {tab === 'deployments' && <div className="stack">
      {currentDeployment && <section className={`panel deployment-live ${currentDeployment.status === 'FAILED' ? 'deployment-live-failed' : currentDeployment.status === 'SUCCESS' ? 'deployment-live-success' : ''}`} aria-live="polite">
        <div className="section-heading">
          <div>
            <span className="eyebrow">{activeDeployment ? 'AKTUALNY DEPLOYMENT' : 'OSTATNI DEPLOYMENT'}</span>
            <h2>{currentDeployment.status === 'FAILED' ? `Błąd podczas: ${deploymentStageLabels[currentDeployment.stage] ?? currentDeployment.stage}` : deploymentStageLabels[currentDeployment.stage] ?? currentDeployment.stage}</h2>
            <p className="muted"><code>{currentDeployment.id.slice(0, 12)}</code>{currentDeployment.job_id ? <> · job <code>{currentDeployment.job_id.slice(0, 12)}</code></> : null}</p>
          </div>
          <span className={`console-badge ${currentDeployment.status === 'FAILED' ? 'badge-danger' : currentDeployment.status === 'SUCCESS' ? 'badge-success' : 'badge-info'}`}>{currentDeployment.status}</span>
        </div>
        <div className="deployment-live-progress">
          <div><span>Postęp</span><strong>{deploymentProgress(currentDeployment)}%</strong></div>
          <progress max={100} value={deploymentProgress(currentDeployment)}>{deploymentProgress(currentDeployment)}%</progress>
        </div>
        <div className="deployment-stage-track">
          {deploymentStages.map((stage) => {
            const currentIndex = deploymentStages.indexOf(currentDeployment.stage as (typeof deploymentStages)[number])
            const stageIndex = deploymentStages.indexOf(stage)
            const current = stage === currentDeployment.stage
            const done = currentDeployment.status === 'SUCCESS' || (currentIndex >= 0 && stageIndex < currentIndex)
            return <div key={stage} className={current ? currentDeployment.status === 'FAILED' ? 'is-failed' : 'is-current' : done ? 'is-done' : ''}><span></span><small>{deploymentStageLabels[stage]}</small></div>
          })}
        </div>
        {currentDeployment.error && <div className="error-banner">{currentDeployment.error}</div>}
      </section>}
      <div className="table-wrap"><table><thead><tr><th>Status</th><th>Stage</th><th>Commit before</th><th>Commit after</th><th>Started</th><th>Duration</th><th>Error</th></tr></thead><tbody>{deployments.map((item) => <tr key={item.id}><td>{item.status}</td><td>{deploymentStageLabels[item.stage] ?? item.stage}</td><td><code>{item.commit_before?.slice(0, 10) || '—'}</code></td><td><code>{item.commit_after?.slice(0, 10) || '—'}</code></td><td>{item.started_at ? new Date(item.started_at).toLocaleString() : '—'}</td><td>{item.duration_ms ? `${item.duration_ms} ms` : '—'}</td><td className="error-cell">{item.error || '—'}</td></tr>)}{deployments.length === 0 && <tr><td colSpan={7} className="muted">Brak deploymentów.</td></tr>}</tbody></table></div>
    </div>}
    {tab === 'logs' && <div className="stack">
      <div className="panel project-logs-toolbar">
        <div>
          <h2>Logi aplikacji</h2>
          <p className="muted">Logi projektu, deploymentów i zadań powiązanych z tą aplikacją.</p>
        </div>
        <div className="project-log-filters">
          <label>Źródło
            <select value={logSource} onChange={(event) => setLogSource(event.target.value)}>
              <option value="all">Wszystkie</option>
              <option value="project">Projekt</option>
              <option value="deployment">Deployment</option>
              <option value="job">Zadania</option>
            </select>
          </label>
          <label>Poziom
            <select value={logLevel} onChange={(event) => setLogLevel(event.target.value)}>
              <option value="">Wszystkie</option>
              <option value="debug">DEBUG</option>
              <option value="info">INFO</option>
              <option value="warn">WARN</option>
              <option value="error">ERROR</option>
            </select>
          </label>
          <label className="project-log-search">Szukaj
            <input
              value={logSearch}
              onChange={(event) => setLogSearch(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  logCursorRef.current = 0
                  void loadLogs()
                }
              }}
              placeholder="Treść komunikatu"
            />
          </label>
          <label className="live-toggle"><input type="checkbox" checked={liveLogs} onChange={(event) => setLiveLogs(event.target.checked)} /> Na żywo</label>
          <button type="button" className="secondary" onClick={() => { logCursorRef.current = 0; void loadLogs() }}>Odśwież</button>
        </div>
      </div>
      <div className="log-console project-log-console">
        {logs.length === 0
          ? <div className="muted">Brak logów dla tej aplikacji.</div>
          : logs.map((entry) => <div className="log-line" key={entry.id}>
              <time>{new Date(entry.created_at).toLocaleString('pl-PL')}</time>
              <span className="log-source">{entry.source}</span>
              <strong>{entry.level.toUpperCase()}</strong>
              <span>{entry.message}</span>
            </div>)}
      </div>
    </div>}
    {tab === 'runtime' && <div className="stack project-tab-content">
      <ProjectRuntimeSection projectId={id} showPorts={false} />
    </div>}
    {tab === 'database' && <div className="stack project-tab-content">
      <ProjectDatabaseSection projectId={id} />
    </div>}
    {tab === 'ports' && <div className="stack project-tab-content">
      <ProjectPortsSection projectId={id} />
    </div>}
    {tab === 'settings' && <div className="stack project-tab-content">
      <form className="panel form-grid project-settings-panel" onSubmit={save}>
        <div className="span-2">
          <h2>Ustawienia aplikacji</h2>
          <p className="muted">Polecenia i zachowanie projektu niezależne od konfiguracji runtime, bazy danych i portów.</p>
        </div>
        <label className="span-2">Working directory<input disabled={user?.role === 'viewer'} value={config.working_directory} onChange={(e) => setConfig({ ...config, working_directory: e.target.value })} /></label>
        <label className="span-2">Build command (własny Docker/Compose)<input disabled={user?.role === 'viewer'} value={config.build_command} onChange={(e) => setConfig({ ...config, build_command: e.target.value })} /></label>
        <label className="span-2">Start command (własny Docker/Compose)<input disabled={user?.role === 'viewer'} value={config.start_command} onChange={(e) => setConfig({ ...config, start_command: e.target.value })} /></label>
        <label className="span-2">Healthcheck<input disabled={user?.role === 'viewer'} value={config.healthcheck} onChange={(e) => setConfig({ ...config, healthcheck: e.target.value })} /></label>
        <label className="checkbox"><input disabled={user?.role === 'viewer'} type="checkbox" checked={config.auto_start} onChange={(e) => setConfig({ ...config, auto_start: e.target.checked })} /> Auto start</label>
        {user?.role !== 'viewer' && <div className="span-2"><button type="submit" disabled={busy !== ''}>{busy === 'save' ? 'Zapisywanie…' : 'Zapisz ustawienia aplikacji'}</button></div>}
      </form>
    </div>}
  </>
}

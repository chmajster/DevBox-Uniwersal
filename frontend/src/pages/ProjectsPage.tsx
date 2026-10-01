import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { request } from '../api/client'
import { listProjects } from '../api/operations'
import type { DockerContainer, Job, Project } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { StatusBadge } from '../components/StatusBadge'
import { jobState } from '../control-room/model'
import { usePolling } from '../control-room/usePolling'
import { readPreference, savePreference } from '../layout/navigation'
import { resolveApplicationRuntimeStatus } from './applicationRuntimeStatus'
import { filterProjects, projectStatuses } from './projectFilters'

type JobCollection = Job[] | { items?: Job[] }
type DockerContainerCollection = DockerContainer[] | { items?: DockerContainer[]; containers?: DockerContainer[] }

async function loadDockerContainers(signal: AbortSignal) {
  const payload = await request<DockerContainerCollection>('/docker/containers', { signal })
  if (Array.isArray(payload)) return payload
  return payload.items ?? payload.containers ?? []
}

async function loadProjectJobs(signal: AbortSignal) {
  const payload = await request<JobCollection>('/jobs?limit=200', { signal })
  return Array.isArray(payload) ? payload : payload.items ?? []
}

function projectJobLabel(type: string) {
  switch (type) {
    case 'project.deploy': return 'Wdrożenie'
    case 'project.git.clone': return 'Klonowanie Git'
    case 'project.git.fetch': return 'Git fetch'
    case 'project.git.pull': return 'Git pull'
    case 'project.git.checkout': return 'Zmiana gałęzi'
    default: return type.startsWith('project.') ? type.slice('project.'.length) : type
  }
}

export function projectApplicationURL(project: Project) {
  const explicit = project.open_url?.trim()
  if (explicit && /^https?:\/\//i.test(explicit)) return explicit
  const domain = project.domain?.trim()
  if (!domain) return ''
  return /^https?:\/\//i.test(domain) ? domain : `http://${domain}`
}

function ProjectDomainLink({ project }: { project: Project }) {
  const url = projectApplicationURL(project)
  if (!project.domain || !url) return <>—</>
  return <a
    href={url}
    target="_blank"
    rel="noopener noreferrer"
    title={`Otwórz aplikację ${project.name}`}
  >
    {project.domain}
  </a>
}

function latestProjectJobs(jobs: Job[]) {
  const result = new Map<string, Job>()
  const ordered = [...jobs].sort((a, b) => (Date.parse(b.created_at) || 0) - (Date.parse(a.created_at) || 0))
  for (const job of ordered) {
    if (!job.project_id || !job.type.startsWith('project.') || result.has(job.project_id)) continue
    result.set(job.project_id, job)
  }
  return result
}

function ProjectLiveStatus({ project, job, unavailable }: { project: Project; job?: Job; unavailable: boolean }) {
  const state = job ? jobState(job.status) : null
  const active = job ? ['queued', 'pending', 'running'].includes(job.status.toLowerCase()) : false
  const rawStage = job?.result?.stage ?? job?.payload?.stage
  const stage = typeof rawStage === 'string' && rawStage.trim() ? rawStage : job ? projectJobLabel(job.type) : ''
  return <div className={`project-live-status${active ? ' is-active' : ''}`} aria-live="polite">
    <div className="project-live-heading">
      <span className="project-live-title"><span className={`live-dot${active ? ' is-pulsing' : ''}`} aria-hidden="true" />Status na żywo</span>
      <StatusBadge status={project.status} />
    </div>
    {unavailable
      ? <div className="project-live-message">Status zadań chwilowo niedostępny.</div>
      : job && state
        ? <div className="project-live-job">
            <div className="project-live-job-copy"><strong>{stage}</strong><span><span className={`console-badge badge-${state.tone}`}>{state.label}</span> <code>{job.id.slice(0, 8)}</code></span></div>
            <Link className="project-live-link" to={`/jobs?job=${encodeURIComponent(job.id)}`}>Podgląd</Link>
          </div>
        : <div className="project-live-message">Brak aktywnych lub ostatnich zadań projektu.</div>}
  </div>
}

export function ProjectsPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const [projects, setProjects] = useState<Project[]>([])
  const [query, setQuery] = useState('')
  const [view, setView] = useState(() => readPreference('devbox-project-view', 'grid') === 'list' ? 'list' : 'grid')
  const [loading, setLoading] = useState(true)
  const [loaded, setLoaded] = useState(false)
  const [reload, setReload] = useState(0)
  const [loadError, setLoadError] = useState('')
  const [operationError, setOperationError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState('')
  const [archiveTarget, setArchiveTarget] = useState<Project | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Project | null>(null)
  const [showArchived, setShowArchived] = useState(false)
  const mutationLock = useRef(false)
  const statusParam = params.get('status') ?? ''
  const status = projectStatuses.some((value) => value === statusParam) ? statusParam : ''
  const canManage = user?.role === 'admin' || user?.role === 'operator'
  const jobListing = usePolling(loadProjectJobs, 2000, loaded)
  const dockerListing = usePolling(loadDockerContainers, 2500, loaded)
  const projectsWithLiveStatus = useMemo(() => {
    const dockerStateAvailable = dockerListing.data !== null && !dockerListing.error
    return projects.map((project) => ({
      ...project,
      status: resolveApplicationRuntimeStatus(project, dockerListing.data ?? [], dockerStateAvailable),
    }))
  }, [projects, dockerListing.data, dockerListing.error])
  const filtered = useMemo(() => filterProjects(projectsWithLiveStatus, query, status), [projectsWithLiveStatus, query, status])
  const jobsByProject = useMemo(() => latestProjectJobs(jobListing.data ?? []), [jobListing.data])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setLoadError('')
    listProjects(showArchived).then((items) => {
      if (!cancelled) { setProjects(items); setLoaded(true) }
    }).catch((cause: unknown) => {
      if (!cancelled) setLoadError(cause instanceof Error ? cause.message : 'Nie udało się pobrać aplikacji.')
    }).finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [reload, showArchived])

  useEffect(() => {
    if (!loaded) return
    let cancelled = false
    let timer: number | undefined
    async function refreshProjectsLive() {
      if (cancelled) return
      if (!document.hidden) {
        try {
          const items = await listProjects(showArchived)
          if (!cancelled) setProjects(items)
        } catch {
          // The initial/manual load owns the visible error state. A transient live refresh must not wipe the current list.
        }
      }
      if (!cancelled) timer = window.setTimeout(() => void refreshProjectsLive(), 2500)
    }
    timer = window.setTimeout(() => void refreshProjectsLive(), 2500)
    return () => { cancelled = true; if (timer !== undefined) window.clearTimeout(timer) }
  }, [loaded, showArchived])

  function changeStatus(value: string) {
    const next = new URLSearchParams(params)
    if (value) next.set('status', value)
    else next.delete('status')
    setParams(next, { replace: true })
  }

  async function mutate(project: Project, action: 'deploy' | 'archive') {
    if (mutationLock.current) return
    mutationLock.current = true
    setBusy(`${project.id}:${action}`)
    setOperationError('')
    setNotice('')
    try {
      await request<Job | Project>(`/projects/${encodeURIComponent(project.id)}/${action}`, { method: 'POST' })
      if (action === 'deploy') {
        jobListing.refresh()
        navigate(`/apps/${encodeURIComponent(project.id)}?tab=deployments`)
        return
      }
      setNotice(`Zarchiwizowano aplikację „${project.name}”.`)
      setArchiveTarget(null)
      setReload((value) => value + 1)
    } catch (cause) {
      setOperationError(cause instanceof Error ? cause.message : 'Operacja nie powiodła się. Spróbuj ponownie.')
    } finally { mutationLock.current = false; setBusy('') }
  }

  async function removeProject(project: Project) {
    if (mutationLock.current) return
    mutationLock.current = true
    setBusy(`${project.id}:delete`)
    setOperationError('')
    setNotice('')
    try {
      await request<{ status: string }>(`/projects/${encodeURIComponent(project.id)}`, { method: 'DELETE' })
      setNotice(`Usunięto aplikację „${project.name}”.`)
      setDeleteTarget(null)
      setReload((value) => value + 1)
    } catch (cause) {
      setOperationError(cause instanceof Error ? cause.message : 'Nie udało się usunąć aplikacji.')
    } finally { mutationLock.current = false; setBusy('') }
  }

  function actions(project: Project) {
    const archived = Boolean(project.archived_at)
    return <div className="project-actions">
      <Link className="project-details-link" to={`/apps/${encodeURIComponent(project.id)}`}>Szczegóły <Icon name="arrow" size={15} /></Link>
      {canManage && !archived && <>
        <button className="secondary-button button-compact" disabled={!!busy} onClick={() => void mutate(project, 'deploy')} aria-label={`Wdróż ${project.name}`}>
          <Icon name="play" size={14} />{busy === `${project.id}:deploy` ? 'Zlecanie…' : 'Wdróż'}
        </button>
        <button className="icon-button archive-action" disabled={!!busy} title={`Archiwizuj ${project.name}`} aria-label={`Archiwizuj ${project.name}`}
          onClick={() => { setOperationError(''); setArchiveTarget(project) }}><Icon name="archive" size={17} /></button>
      </>}
      {user?.role === 'admin' && <button className="icon-button danger" disabled={!!busy} title={`Usuń ${project.name}`} aria-label={`Usuń ${project.name}`}
        onClick={() => { setOperationError(''); setDeleteTarget(project) }}><Icon name="trash" size={17} /></button>}
    </div>
  }

  return <>
    <div className="page-heading workspace-heading">
      <div><span className="eyebrow">WORKSPACE</span><h1>Aplikacje</h1><p className="muted">Od kodu do działającej aplikacji. Wszystko w jednym miejscu.</p></div>
      <div className="heading-actions"><button className="secondary-button" disabled={loading} onClick={() => { setReload((value) => value + 1); dockerListing.refresh(); jobListing.refresh() }}><Icon name="refresh" size={17} />Odśwież</button>
        {user?.role === 'admin' && <Link className="button-link secondary-button" to="/script-apps"><Icon name="code" size={17} />Instalator URL / curl</Link>}{canManage && <Link className="button-link" to="/apps/new"><Icon name="plus" size={18} />Dodaj aplikację</Link>}
      </div>
    </div>
    <div className="project-toolbar">
      <label className="search-field"><Icon name="search" size={18} /><input aria-label="Szukaj aplikacji" placeholder="Szukaj po nazwie, runtime, domenie…" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
      <label className="status-filter"><span className="sr-only">Filtr statusu</span><select value={status} onChange={(event) => changeStatus(event.target.value)}><option value="">Wszystkie statusy</option>{projectStatuses.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
      {user?.role === 'admin' && <label className="checkbox"><input type="checkbox" checked={showArchived} onChange={(event) => setShowArchived(event.target.checked)} /> Pokaż zarchiwizowane</label>}
      <div className="view-switch" role="group" aria-label="Widok aplikacji">
        <button className="icon-button" aria-label="Widok kafelków" aria-pressed={view === 'grid'} onClick={() => { setView('grid'); savePreference('devbox-project-view', 'grid') }}><Icon name="apps" size={18} /></button>
        <button className="icon-button" aria-label="Widok tabeli" aria-pressed={view === 'list'} onClick={() => { setView('list'); savePreference('devbox-project-view', 'list') }}><Icon name="list" size={18} /></button>
      </div>
    </div>
    <div className="collection-summary"><span role="status">{loading ? 'Pobieranie aplikacji…' : loaded ? `${filtered.length} z ${projects.length} aplikacji` : 'Dane niedostępne'}</span>{(query || status) && <button className="text-button" onClick={() => { setQuery(''); changeStatus('') }}>Wyczyść filtry</button>}</div>
    {loadError && <div className="error-banner" role="alert">{loadError} <button className="secondary-button button-compact" disabled={loading} onClick={() => setReload((value) => value + 1)}>Spróbuj ponownie</button></div>}
    {operationError && !archiveTarget && <div className="error-banner" role="alert">{operationError}</div>}
    {notice && <div className="success-banner" role="status">{notice} <Link to="/jobs">Przejdź do zadań</Link></div>}
    {loading && !loaded && <div className="project-grid" aria-hidden="true">{[1, 2, 3].map((key) => <div className="skeleton project-skeleton" key={key} />)}</div>}
    {loaded && filtered.length > 0 && <div aria-busy={loading}>
      {view === 'grid' ? <div className="project-grid">{filtered.map((project) => <article className="project-card" key={project.id}>
        <div className="project-card-top"><span className="project-symbol"><Icon name="code" size={23} /></span><StatusBadge status={project.status} /></div>
        <h2><Link to={`/apps/${encodeURIComponent(project.id)}`}>{project.name}</Link></h2>
        <p className="project-description">{project.description || project.domain || 'Projekt zarządzany przez DevBox'}</p>
        <div className="project-tags"><span>{project.runtime || 'Runtime nieustawiony'}</span><span>{project.container_policy === 'generated_compose' ? 'DevBox Compose' : project.container_policy === 'custom' ? 'Własny Docker' : 'Kontener zarządzany'}</span></div>
        <dl className="project-meta"><div><dt><Icon name="branch" size={14} />Gałąź</dt><dd>{project.branch || '—'}</dd></div><div><dt><Icon name="network" size={14} />Port</dt><dd>{project.port ?? '—'}</dd></div><div><dt><Icon name="globe" size={14} />Domena</dt><dd><ProjectDomainLink project={project} /></dd></div><div><dt><Icon name="code" size={14} />Commit</dt><dd><code>{project.current_commit?.slice(0, 10) || '—'}</code></dd></div></dl>
        <ProjectLiveStatus project={project} job={jobsByProject.get(project.id)} unavailable={Boolean(jobListing.error)} />
        {actions(project)}
      </article>)}</div> : <div className="table-wrap"><table><caption className="sr-only">Aplikacje i dostępne operacje</caption><thead><tr>{['Nazwa', 'Status', 'Live', 'Runtime', 'Gałąź', 'Port', 'Domena', 'Commit', 'Akcje'].map((label) => <th key={label} scope="col">{label}</th>)}</tr></thead><tbody>{filtered.map((project) => <tr key={project.id}>
        <td><Link className="project-name" to={`/apps/${encodeURIComponent(project.id)}`}>{project.name}</Link></td><td><StatusBadge status={project.status} /></td><td>{jobsByProject.get(project.id) ? <Link className="table-live-link" to={`/jobs?job=${encodeURIComponent(jobsByProject.get(project.id)!.id)}`}>{jobState(jobsByProject.get(project.id)!.status).label}</Link> : '—'}</td><td>{project.runtime || '—'}</td><td>{project.branch || '—'}</td><td>{project.port ?? '—'}</td><td><ProjectDomainLink project={project} /></td><td><code>{project.current_commit?.slice(0, 10) || '—'}</code></td><td>{actions(project)}</td>
      </tr>)}</tbody></table></div>}
    </div>}
    {loaded && !loading && !loadError && filtered.length === 0 && <div className="workspace-empty">
      <span className="empty-icon"><Icon name={projects.length ? 'search' : 'apps'} size={28} /></span><h2>{projects.length ? 'Brak pasujących aplikacji' : 'Miejsce na Twoją pierwszą aplikację'}</h2>
      <p>{projects.length ? 'Zmień wyszukiwanie lub usuń filtry.' : 'Podłącz repozytorium Git albo wybierz lokalny katalog projektu.'}</p>
      {projects.length > 0 ? <button className="secondary-button" onClick={() => { setQuery(''); changeStatus('') }}>Wyczyść filtry</button> : canManage && <Link className="button-link" to="/apps/new"><Icon name="plus" size={17} />Dodaj aplikację</Link>}
    </div>}
    <Modal open={archiveTarget !== null} onClose={() => { if (!busy) setArchiveTarget(null) }} labelId="archive-title" className="confirm-dialog">
      <span className="empty-icon warning-icon"><Icon name="archive" size={26} /></span><h2 id="archive-title">Archiwizować aplikację?</h2>
      <p>Potwierdź archiwizację aplikacji <strong>{archiveTarget?.name}</strong> w DevBox.</p>
      {operationError && <div role="alert" className="error-banner">{operationError}</div>}
      <div className="modal-actions"><button className="secondary-button" disabled={!!busy} onClick={() => setArchiveTarget(null)}>Anuluj</button><button className="danger" disabled={!!busy} onClick={() => { if (archiveTarget) void mutate(archiveTarget, 'archive') }}>{busy ? 'Archiwizowanie…' : 'Archiwizuj aplikację'}</button></div>
    </Modal>
    <Modal open={deleteTarget !== null} onClose={() => { if (!busy) setDeleteTarget(null) }} labelId="delete-project-title" className="confirm-dialog">
      <span className="empty-icon warning-icon"><Icon name="trash" size={26} /></span><h2 id="delete-project-title">Usunąć aplikację?</h2>
      <p>Ta operacja trwale usunie wpis projektu <strong>{deleteTarget?.name}</strong> i powiązane dane DevBox. Pliki lokalnego katalogu projektu nie są kasowane przez ten przycisk.</p>
      {operationError && <div role="alert" className="error-banner">{operationError}</div>}
      <div className="modal-actions"><button className="secondary-button" disabled={!!busy} onClick={() => setDeleteTarget(null)}>Anuluj</button><button className="danger" disabled={!!busy} onClick={() => { if (deleteTarget) void removeProject(deleteTarget) }}>{busy ? 'Usuwanie…' : 'Usuń trwale'}</button></div>
    </Modal>
  </>
}

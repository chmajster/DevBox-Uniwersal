import { useEffect, useState } from 'react'
import { NavLink, useNavigate, useParams } from 'react-router-dom'
import { getProject, getProjectTool, runProjectAction } from '../api/operations'
import { request } from '../api/client'
import type { Job, LogEntry, Project, ProjectRuntimeInfo } from '../api/types'
import { ErrorState } from '../components/ErrorState'
import { StatusBadge } from '../components/StatusBadge'
import { PROJECT_TABS, type ProjectTab } from '../routes'
import { ProjectDatabaseSection } from '../runtime/ProjectDatabaseSection'
import { ProjectPHPModulesSection } from '../runtime/ProjectPHPModulesSection'

const tabLabels: Record<ProjectTab, string> = {
  overview: 'Overview',
  configuration: 'Configuration',
  runtime: 'Runtime',
  'php-modules': 'Moduły PHP',
  git: 'Git',
  deployments: 'Deployments',
  logs: 'Logs',
  environment: 'Environment',
  database: 'Database',
  networking: 'Networking',
  backups: 'Backups'
}

function field(label: string, value: unknown) {
  const display = value === undefined || value === null || value === '' ? '—' : String(value)
  return <div className="detail-field"><span>{label}</span><strong>{display}</strong></div>
}

function ResourcePanel({ path }: { path: string }) {
  const [data, setData] = useState<unknown>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    request<unknown>(path)
      .then((value) => { if (!cancelled) setData(value) })
      .catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { cancelled = true }
  }, [path])

  if (error) return <ErrorState message={error} />
  if (data === null) return <p className="muted">Loading…</p>
  return <pre className="resource-json">{JSON.stringify(data, null, 2)}</pre>
}

function ProjectLogs({ projectID }: { projectID: string }) {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    request<LogEntry[]>(`/logs?source=project&project=${encodeURIComponent(projectID)}&limit=200`)
      .then((items) => { if (!cancelled) setLogs(items) })
      .catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { cancelled = true }
  }, [projectID])

  if (error) return <ErrorState message={error} />
  return <div className="log-console">
    {logs.length === 0
      ? <div className="muted">No project logs.</div>
      : logs.map((entry) => <div className="log-line" key={entry.id}>
          <time>{new Date(entry.created_at).toLocaleString()}</time>
          <strong>{entry.level.toUpperCase()}</strong>
          <span>{entry.message}</span>
        </div>)}
  </div>
}

function EnvironmentPanel({ environment }: { environment?: Record<string, string> }) {
  const entries = Object.entries(environment ?? {})
  if (entries.length === 0) return <p className="muted">No environment variables returned by the project API.</p>
  return <div className="detail-grid">
    {entries.map(([key, value]) => {
      const sensitive = /(password|secret|token|private|key)/i.test(key)
      return <div key={key}>{field(key, sensitive ? '••••••••' : value)}</div>
    })}
  </div>
}

function TabContent({ project, tab }: { project: Project; tab: ProjectTab }) {
  switch (tab) {
    case 'overview':
      return <div className="detail-grid">
        {field('Name', project.name)}
        {field('Status', project.status)}
        {field('Runtime', project.runtime)}
        {field('Runtime version', project.runtime_version)}
        {field('Container policy', project.container_policy === 'custom' ? 'Custom Docker' : 'Managed Docker')}
        {field('Working directory', project.working_directory ?? project.local_path)}
        {field('Healthcheck', project.healthcheck)}
      </div>
    case 'configuration':
      return <div className="detail-grid">
        {field('Source type', project.source_type)}
        {field('Build command', project.build_command)}
        {field('Start command', project.start_command)}
        {field('Auto start', project.auto_start === undefined ? undefined : project.auto_start ? 'yes' : 'no')}
        {field('Branch', project.branch)}
        {field('Local path', project.local_path)}
      </div>
    case 'runtime':
      return <div className="stack">
        <div className="detail-grid">
          {field('Runtime', project.runtime)}
          {field('Status', project.status)}
          {field('Start command', project.start_command)}
          {field('Healthcheck', project.healthcheck)}
        </div>
      </div>
    case 'git':
      return <div className="detail-grid">
        {field('Provider', project.source?.provider)}
        {field('Repository', project.repository_url ?? project.source?.repository_url)}
        {field('Reference', project.branch ?? project.source?.reference)}
      </div>
    case 'deployments':
      return <ResourcePanel path={`/projects/${encodeURIComponent(project.id)}/deployments`} />
    case 'logs':
      return <ProjectLogs projectID={project.id} />
    case 'environment':
      return <EnvironmentPanel environment={project.environment} />
    case 'database':
      return <ProjectDatabaseSection projectId={project.id} />
    case 'php-modules':
      return <ProjectPHPModulesSection projectId={project.id} runtimeHint={project.runtime} />
    case 'networking':
      return <ResourcePanel path={`/networking/ports?project_id=${encodeURIComponent(project.id)}`} />
    case 'backups':
      return <ResourcePanel path={`/projects/${encodeURIComponent(project.id)}/backups`} />
  }
}

export function ProjectDetailsPage() {
  const params = useParams()
  const navigate = useNavigate()
  const projectID = params.projectId ?? ''
  const requestedTab = (params.tab ?? 'overview') as ProjectTab
  const tab = PROJECT_TABS.includes(requestedTab) ? requestedTab : 'overview'
  const [project, setProject] = useState<Project | null>(null)
  const [detectedRuntime, setDetectedRuntime] = useState('')
  const [error, setError] = useState('')
  const [actionError, setActionError] = useState('')
  const [activeAction, setActiveAction] = useState('')
  const [job, setJob] = useState<Job | null>(null)

  useEffect(() => {
    let cancelled = false
    Promise.all([
      getProject(projectID),
      request<ProjectRuntimeInfo>(`/projects/${encodeURIComponent(projectID)}/runtime`).catch(() => null),
    ])
      .then(([value, runtimeInfo]) => {
        if (cancelled) return
        setProject(value)
        setDetectedRuntime(runtimeInfo?.runtime ?? '')
      })
      .catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { cancelled = true }
  }, [projectID])

  async function run(action: 'start' | 'stop' | 'restart' | 'deploy') {
    setActionError('')
    setActiveAction(action)
    try {
      const createdJob = await runProjectAction(projectID, action)
      setJob(createdJob)
    } catch (reason) {
      setActionError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setActiveAction('')
    }
  }

  async function openTool(tool: 'open' | 'terminal') {
    setActionError('')
    try {
      const result = await getProjectTool(projectID, tool)
      window.open(result.url, '_blank', 'noopener,noreferrer')
    } catch (reason) {
      setActionError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  if (error) return <ErrorState message={error} />
  if (!project) return <p className="muted">Loading project…</p>

  return <>
    <div className="page-heading project-heading">
      <div>
        <div className="heading-status"><h1>{project.name}</h1><StatusBadge status={project.status} /></div>
        <p className="muted">{project.description ?? project.id}</p>
      </div>
      <div className="action-bar">
        <button type="button" disabled={Boolean(activeAction)} onClick={() => void run('start')}>Start</button>
        <button type="button" disabled={Boolean(activeAction)} onClick={() => void run('stop')}>Stop</button>
        <button type="button" disabled={Boolean(activeAction)} onClick={() => void run('restart')}>Restart</button>
        <button type="button" disabled={Boolean(activeAction)} onClick={() => void run('deploy')}>Deploy</button>
        <button className="secondary-button" type="button" onClick={() => void openTool('open')}>Open</button>
        <button className="secondary-button" type="button" onClick={() => navigate(`/projects/${encodeURIComponent(projectID)}/logs`)}>Logs</button>
        <button className="secondary-button" type="button" onClick={() => void openTool('terminal')}>Terminal</button>
      </div>
    </div>
    {activeAction && <div className="operation-state">Operation requested: {activeAction}</div>}
    {actionError && <ErrorState message={actionError} />}
    {job && <div className="operation-state">Job {job.id}: {job.status}</div>}

    <nav className="tabs" aria-label="Project details">
      {PROJECT_TABS.filter((tabName) => tabName !== 'php-modules' || [project.runtime, detectedRuntime].some((value) => value.trim().toLowerCase() === 'php')).map((tabName) => (
        <NavLink key={tabName} to={`/projects/${encodeURIComponent(projectID)}/${tabName}`}>
          {tabLabels[tabName]}
        </NavLink>
      ))}
    </nav>

    <section className="tab-panel">
      <TabContent project={project} tab={tab} />
    </section>
  </>
}

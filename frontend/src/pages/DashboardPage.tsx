import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getMonitoringSnapshot, getServiceProbe, listDatabases, listDockerContainers, listPorts, listProjects } from '../api/operations'
import type { MonitoringSnapshot, Project, ServiceProbe } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Icon, type IconName } from '../components/Icon'
import { StatusBadge } from '../components/StatusBadge'
import { normalizeOperationalStatus } from '../status'

interface DashboardState {
  monitoring: MonitoringSnapshot | null
  projects: Project[] | null
  dockerContainers: number | null
  databases: number | null
  ports: number | null
  services: ServiceProbe[]
  errors: string[]
}
const initialState: DashboardState = { monitoring: null, projects: null, dockerContainers: null, databases: null, ports: null, services: [], errors: [] }
const reasonMessage = (reason: unknown) => reason instanceof Error ? reason.message : String(reason)
const countValue = (value: number | null) => value === null ? '—' : String(value)

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / 1024 ** index).toFixed(index >= 3 ? 1 : 0)} ${units[index]}`
}
function formatUptime(seconds: number) {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  return days > 0 ? `${days} dni ${hours} godz.` : `${hours} godz. ${Math.floor((seconds % 3600) / 60)} min`
}
function ResourceMeter({ label, icon, value, detail }: { label: string; icon: IconName; value?: number; detail?: string }) {
  const available = value !== undefined && Number.isFinite(value)
  const percent = available ? Math.max(0, Math.min(100, value)) : 0
  return <div className="resource-meter"><span className="resource-icon"><Icon name={icon} size={20} /></span><div className="resource-body"><div><strong>{label}</strong><span>{available ? `${percent.toFixed(1)}%` : 'Brak danych'}</span></div>
    {available ? <progress max={100} value={percent} aria-label={`Wykorzystanie ${label}`} className={percent >= 85 ? 'meter-warning' : ''} /> : <div className="meter-unavailable" />}
    <small>{detail || 'Pomiar niedostępny'}</small></div></div>
}

export function DashboardPage() {
  const { user } = useAuth()
  const [state, setState] = useState<DashboardState>(initialState)
  const [loading, setLoading] = useState(true)
  const [reload, setReload] = useState(0)
  const [updatedAt, setUpdatedAt] = useState('')
  const canManage = user?.role === 'admin' || user?.role === 'operator'

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    async function load() {
      const [monitoring, projects, containers, databases, ports, nginx, mysql, docker, devbox] = await Promise.allSettled([
        getMonitoringSnapshot(), listProjects(), listDockerContainers(), listDatabases(), listPorts(),
        getServiceProbe('Nginx'), getServiceProbe('MySQL'), getServiceProbe('Docker'), getServiceProbe('DevBox')
      ])
      if (cancelled) return
      const errors: string[] = []
      const services: ServiceProbe[] = []
      for (const [name, result] of [['DevBox', devbox], ['Docker', docker], ['Nginx', nginx], ['MySQL', mysql]] as const) {
        if (result.status === 'fulfilled') services.push(result.value)
        else { const message = reasonMessage(result.reason); services.push({ name, status: 'UNHEALTHY', message }); errors.push(`${name}: ${message}`) }
      }
      for (const [name, result] of [['Monitoring', monitoring], ['Aplikacje', projects], ['Kontenery', containers], ['Bazy danych', databases], ['Porty', ports]] as const) {
        if (result.status === 'rejected') errors.push(`${name}: ${reasonMessage(result.reason)}`)
      }
      setState({
        monitoring: monitoring.status === 'fulfilled' ? monitoring.value : null,
        projects: projects.status === 'fulfilled' ? projects.value : null,
        dockerContainers: containers.status === 'fulfilled' ? containers.value.length : null,
        databases: databases.status === 'fulfilled' ? databases.value.length : null,
        ports: ports.status === 'fulfilled' ? ports.value.length : null,
        services, errors
      })
      setUpdatedAt(new Date().toLocaleTimeString('pl-PL'))
      setLoading(false)
    }
    void load()
    return () => { cancelled = true }
  }, [reload])

  const projects = state.projects
  const normalized = projects?.map((project) => normalizeOperationalStatus(project.status))
  const running = normalized?.filter((status) => status === 'RUNNING').length ?? null
  const stopped = normalized?.filter((status) => status === 'STOPPED').length ?? null
  const failed = normalized?.filter((status) => status === 'FAILED' || status === 'UNHEALTHY').length ?? null
  const monitoring = state.monitoring
  const kpis: { label: string; value: number | null; detail: string; icon: IconName; to: string }[] = [
    { label: 'Aplikacje', value: projects?.length ?? null, detail: projects ? `${stopped} zatrzymanych · ${failed} do sprawdzenia` : 'Odczyt z API projektów', icon: 'apps', to: '/apps' },
    { label: 'Działające', value: running, detail: 'Aplikacje ze statusem RUNNING', icon: 'activity', to: '/apps?status=RUNNING' },
    { label: 'Kontenery', value: state.dockerContainers, detail: 'Kontenery w Docker Engine', icon: 'box', to: '/docker' },
    { label: 'Bazy danych', value: state.databases, detail: 'Zarządzane bazy danych', icon: 'database', to: '/databases' }
  ]

  return <>
    <div className="page-heading workspace-heading"><div><span className="eyebrow">TWOJE CENTRUM ZARZĄDZANIA</span><h1>Przegląd</h1><p className="muted">Aplikacje, infrastruktura i operacje w jednym miejscu.</p></div>
      <div className="heading-actions"><button className="secondary-button" disabled={loading} onClick={() => setReload((value) => value + 1)}><Icon name="refresh" size={17} />{loading ? 'Odświeżanie…' : 'Odśwież'}</button>{canManage && <Link className="button-link" to="/apps/new"><Icon name="plus" size={18} />Dodaj aplikację</Link>}</div>
    </div>
    <div className="dashboard-caption" role="status"><span className="caption-label"><Icon name="clock" size={14} />{loading ? 'Pobieranie aktualnych danych…' : `Ostatni odczyt: ${updatedAt}`}</span><span>Dane z podłączonych modułów</span></div>
    <div className="overview-kpis" aria-busy={loading}>{kpis.map((kpi) => <Link className="overview-kpi" to={kpi.to} key={kpi.label}>
      <div className="kpi-title"><span>{kpi.label}</span><span className="kpi-icon"><Icon name={kpi.icon} size={20} /></span></div>
      <strong className={loading && !updatedAt ? 'kpi-loading' : ''}>{countValue(kpi.value)}</strong><div className="kpi-detail"><span>{kpi.detail}</span><Icon name="arrow" size={16} /></div>
    </Link>)}</div>
    <div className="dashboard-columns">
      <section className="workspace-card host-card"><div className="card-heading"><div><span className="eyebrow">INFRASTRUKTURA</span><h2>Zasoby hosta</h2></div><span className="subtle-tag">CPU / RAM / Dysk</span></div>
        <ResourceMeter label="CPU" icon="cpu" value={monitoring?.cpu.available ? monitoring.cpu.usage_percent : undefined} detail={monitoring ? `${monitoring.cpu.cores} rdzeni procesora` : undefined} />
        <ResourceMeter label="RAM" icon="memory" value={monitoring?.memory.available ? monitoring.memory.usage_percent : undefined} detail={monitoring?.memory.available ? `${formatBytes(monitoring.memory.used_bytes)} z ${formatBytes(monitoring.memory.total_bytes)}` : undefined} />
        <ResourceMeter label="Dysk" icon="disk" value={monitoring?.disk.available ? monitoring.disk.usage_percent : undefined} detail={monitoring?.disk.available ? `${formatBytes(monitoring.disk.used_bytes)} z ${formatBytes(monitoring.disk.total_bytes)}` : undefined} />
        <div className="host-footer"><span><Icon name="clock" size={15} />Uptime <strong>{monitoring ? formatUptime(monitoring.host_uptime_seconds) : '—'}</strong></span><Link to="/ports">Porty: {countValue(state.ports)} <Icon name="arrow" size={14} /></Link></div>
        {monitoring && <p className="host-processes muted">Procesy hosta: {monitoring.process.host_process_count}</p>}
      </section>
      <section className="workspace-card services-card"><div className="card-heading"><div><span className="eyebrow">KOMPONENTY</span><h2>Status usług</h2></div><Icon name="activity" size={21} /></div>
        {loading && state.services.length === 0 ? <div className="service-loading" role="status">Sprawdzanie usług…</div> : <div className="service-list">{state.services.map((service) => <article className="workspace-service" key={service.name}>
          <span className="service-symbol"><Icon name={service.name === 'MySQL' ? 'database' : service.name === 'Nginx' ? 'globe' : service.name === 'Docker' ? 'box' : 'code'} size={20} /></span>
          <div><strong>{service.name}</strong><small>{service.message || 'Odpowiedź z modułu API'}</small></div><StatusBadge status={service.status} />
        </article>)}</div>}
        <Link className="card-footer-link" to="/health">Monitoring aplikacji <Icon name="arrow" size={16} /></Link>
      </section>
    </div>
    <section className="workspace-card recent-card"><div className="card-heading"><div><span className="eyebrow">PROJEKTY</span><h2>Twoje aplikacje</h2></div><Link className="inline-link" to="/apps">Wszystkie aplikacje <Icon name="arrow" size={16} /></Link></div>
      {projects === null ? <p className="empty-hint">{loading ? 'Pobieranie aplikacji…' : 'Nie udało się pobrać aplikacji. Użyj przycisku Odśwież.'}</p> : projects.length === 0 ? <div className="dashboard-empty"><span className="empty-icon"><Icon name="apps" size={25} /></span><div><h3>Zacznij od pierwszej aplikacji</h3><p>Podłącz Git lub lokalny katalog, aby zarządzać projektem.</p></div>{canManage && <Link className="secondary-button button-link" to="/apps/new">Dodaj aplikację <Icon name="plus" size={16} /></Link>}</div> : <div className="recent-apps">{projects.slice(0, 5).map((project) => <Link to={`/apps/${encodeURIComponent(project.id)}`} key={project.id}>
        <span className="project-symbol"><Icon name="code" size={19} /></span><span className="recent-name"><strong>{project.name}</strong><small>{project.domain || project.description || project.source_type}</small></span><span className="subtle-tag recent-runtime">{project.runtime || '—'}</span><StatusBadge status={project.status} /><Icon name="chevron" size={16} />
      </Link>)}</div>}
    </section>
    <div className="quick-links">{([{ to: '/jobs', title: 'Kolejka zadań', detail: 'Postęp wdrożeń i operacji', icon: 'jobs' }, { to: '/logs', title: 'Centralne logi', detail: 'Zdarzenia i diagnostyka', icon: 'logs' }, { to: '/health', title: 'Monitoring aplikacji', detail: 'Stan i historia kontroli', icon: 'activity' }] as const).map((item) => <Link to={item.to} key={item.to}><span className="quick-icon"><Icon name={item.icon} /></span><span><strong>{item.title}</strong><small>{item.detail}</small></span><Icon name="arrow" size={17} /></Link>)}</div>
    {(state.errors.length > 0 || !!monitoring?.warnings?.length) && <section className="dashboard-errors" aria-label="Problemy z odczytem"><h2>Wymagają uwagi</h2>{[...state.errors, ...(monitoring?.warnings ?? [])].map((message, index) => <div className="error-banner" role="alert" key={`${index}:${message}`}>{message}</div>)}</section>}
  </>
}

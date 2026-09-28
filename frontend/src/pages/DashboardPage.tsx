import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { Icon, type IconName } from '../components/Icon'
import { StatusBadge } from '../components/StatusBadge'
import { useControlRoom } from '../control-room/ControlRoomContext'
import { LogPreview } from '../control-room/LogPreview'
import { MetricHistory } from '../control-room/MetricHistory'
import { ResourceGauge } from '../control-room/ResourceGauge'
import { bytes, jobState, pendingJobs, recent, time, uptime } from '../control-room/model'
import { normalizeOperationalStatus } from '../status'

function PanelHeading({ title, icon, to, link = 'Szczegóły' }: { title: string; icon: IconName; to?: string; link?: string }) {
  return <div className="console-panel-heading"><h2><Icon name={icon} size={19} />{title}</h2>{to && <Link to={to}>{link} <Icon name="chevron" size={13} /></Link>}</div>
}
function MiniStat({ icon, label, children }: { icon: IconName; label: string; children: ReactNode }) {
  return <div><dt><Icon name={icon} size={14} />{label}</dt><dd>{children}</dd></div>
}
function Clock() {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => { const timer = window.setInterval(() => setNow(new Date()), 1000); return () => clearInterval(timer) }, [])
  return <div className="console-clock"><span>{now.toLocaleDateString('pl-PL', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })}</span><time dateTime={now.toISOString()}>{now.toLocaleTimeString('pl-PL', { hour: '2-digit', minute: '2-digit' })}</time></div>
}
export function DashboardPage() {
  const { user } = useAuth()
  const { overview, monitoring, history } = useControlRoom()
  const data = overview.data
  const host = monitoring.error ? null : monitoring.data
  const services = data?.services ?? []
  const projects = data?.projects
  const containers = data?.containers
  const jobs = data?.jobs
  const appRunning = projects?.filter((item) => normalizeOperationalStatus(item.status) === 'RUNNING').length
  const appStopped = projects?.filter((item) => normalizeOperationalStatus(item.status) === 'STOPPED').length
  const containerRunning = containers?.filter((item) => item.state.toLowerCase() === 'running').length
  const queued = jobs?.filter((job) => ['queued', 'pending'].includes(job.status.toLowerCase())).length
  const runningJobs = jobs?.filter((job) => job.status.toLowerCase() === 'running').length
  const queue = jobs ? pendingJobs(jobs) : null
  const docker = services.find((item) => item.to === '/docker')
  const warnings = [...(data?.errors ?? []), overview.error, monitoring.error, ...(host?.warnings ?? [])].filter(Boolean)
  const busy = overview.loading || monitoring.loading
  function refresh() { overview.refresh(); monitoring.refresh() }
  const summary = [
    { label: 'Aplikacje', icon: 'apps' as const, value: projects?.length, to: '/apps', detail: projects ? `${appRunning} uruchomione · ${appStopped} zatrzymane` : 'Brak odczytu aplikacji' },
    { label: 'Kontenery', icon: 'box' as const, value: containers?.length, to: '/docker', detail: containers ? `${containerRunning} uruchomionych` : 'Brak odczytu Docker' },
    { label: 'Bazy danych', icon: 'database' as const, value: data?.databases?.length, to: '/databases', detail: data?.databases ? 'Zarządzane bazy danych' : 'Brak odczytu baz danych' },
    { label: 'Zadania w kolejce', icon: 'activity' as const, value: queue?.length, to: '/jobs?status=active', detail: jobs ? `${runningJobs} w toku · ${queued} oczekuje` : 'Brak odczytu zadań' }
  ]
  return <div className="control-room">
    <div className="console-heading"><div><h1>Przegląd</h1><p>Twoje lokalne centrum zarządzania. Wszystko pod kontrolą.</p></div><div className="console-heading-actions"><Clock /><button className="secondary-button" disabled={busy} onClick={refresh}><Icon name="refresh" size={17} />Odśwież</button>{(user?.role === 'admin' || user?.role === 'operator') && <Link className="button-link" to="/apps/new"><Icon name="plus" size={18} />Dodaj aplikację</Link>}</div></div>
    <div className="console-kpis" aria-busy={overview.loading}>{summary.map((item, index) => <Link className={`console-kpi kpi-${index}`} to={item.to} key={item.label}><span className="console-kpi-icon"><Icon name={item.icon} size={22} /></span><div><span>{item.label}</span><strong>{item.value ?? '—'}</strong><small><span className={`status-dot ${item.value === undefined ? 'dot-unknown' : ''}`} />{item.detail}</small></div></Link>)}</div>
    <div className="console-grid">
      <section className="console-panel environment-panel"><PanelHeading title="Stan środowiska" icon="activity" to="#host-monitoring" />
        <div className="environment-content"><div className="resource-gauges">
          <ResourceGauge label="CPU" tone="cpu" value={host?.cpu.available ? host.cpu.usage_percent : undefined} detail={host ? `${host.cpu.cores} rdzeni` : 'Brak odczytu CPU'} />
          <ResourceGauge label="RAM" tone="memory" value={host?.memory.available ? host.memory.usage_percent : undefined} detail={host?.memory.available ? `${bytes(host.memory.used_bytes)} / ${bytes(host.memory.total_bytes)}` : 'Brak odczytu RAM'} />
          <ResourceGauge label="Dysk" tone="disk" value={host?.disk.available ? host.disk.usage_percent : undefined} detail={host?.disk.available ? `${bytes(host.disk.used_bytes)} / ${bytes(host.disk.total_bytes)}` : 'Brak odczytu dysku'} />
        </div><dl className="environment-facts">
          <MiniStat icon="clock" label="Czas działania">{uptime(host?.host_uptime_seconds)}</MiniStat><MiniStat icon="apps" label="Procesy">{host?.process.host_process_count ?? '—'}</MiniStat>
          <MiniStat icon="network" label="Przydzielone porty"><Link to="/ports">{data?.ports?.length ?? '—'}</Link></MiniStat>
          <MiniStat icon="box" label="Docker Engine"><span className={docker?.state === 'running' ? 'text-success' : 'muted'}>{docker?.state === 'running' ? 'Działa' : docker?.state === 'unhealthy' ? 'Niedostępny' : 'Brak odczytu'}</span></MiniStat>
          <MiniStat icon="disk" label="Wolne miejsce">{host?.disk.available ? bytes(host.disk.free_bytes) : '—'}</MiniStat>
          <MiniStat icon="cpu" label="Temperatura CPU"><span className="muted" title="Aktualne API nie udostępnia temperatury CPU">Brak danych</span></MiniStat>
        </dl></div>
      </section>
      <section className="console-panel console-services" id="service-status"><PanelHeading title="Status usług" icon="box" />
        {services.length === 0 && <p className="console-empty">{overview.loading ? 'Sprawdzanie usług…' : 'Statusy usług niedostępne.'}</p>}
        {services.map((service) => <div className="console-service" key={service.name}><span className="service-icon"><Icon name={service.to === '/docker' ? 'box' : service.to === '/databases' ? 'database' : service.to === '/domains' ? 'globe' : 'code'} size={21} /></span>
          <Link className="service-title" to={service.to}><strong>{service.name}</strong><small title={service.detail}>{service.detail}</small></Link><span className={`console-badge badge-${service.state === 'running' ? 'success' : service.state === 'unknown' ? 'warning' : 'danger'}`}><span className="status-dot" />{service.state === 'running' ? 'RUNNING' : service.state === 'unknown' ? 'NIEZNANY' : 'BŁĄD'}</span>
          <span className="service-latency" title="Czas odczytu endpointu API z przeglądarki, nie opóźnienie samej usługi">{service.elapsed === null ? '—' : `${service.elapsed} ms`}</span>
        </div>)}
      </section>
      <section className="console-panel console-applications"><PanelHeading title="Ostatnie aplikacje" icon="box" to="/apps" link="Wszystkie aplikacje" />
        <div className="console-table-scroll"><table><caption className="sr-only">Ostatnio dodane aplikacje</caption><thead><tr>{['Nazwa', 'Status', 'Runtime', 'Domena', 'Port', 'Dodano'].map((name) => <th scope="col" key={name}>{name}</th>)}</tr></thead><tbody>
          {recent(projects ?? []).slice(0, 4).map((project) => <tr key={project.id}><td><Link className="console-app-name" to={`/apps/${encodeURIComponent(project.id)}`}><span className="service-icon"><Icon name="code" size={19} /></span><span><strong>{project.name}</strong><small>{project.description || project.source_type}</small></span></Link></td><td><StatusBadge status={project.status} /></td><td><span className="runtime-tag">{project.runtime || '—'}</span></td><td className="clip-cell" title={project.domain}>{project.domain || '—'}</td><td>{project.port ?? '—'}</td><td title={time(project.created_at, true)}>{time(project.created_at).slice(0, 5)}</td></tr>)}
          {!projects?.length && <tr><td colSpan={6} className="console-empty">{projects === null || projects === undefined ? 'Brak odczytu aplikacji.' : 'Brak aplikacji. Dodaj projekt Git lub katalog lokalny.'}</td></tr>}
        </tbody></table></div>
      </section>
      <section className="console-panel console-jobs"><PanelHeading title="Kolejka zadań" icon="jobs" to="/jobs" link="Wszystkie zadania" />
        <div className="console-table-scroll"><table><caption className="sr-only">Najnowsze zadania i wyniki</caption><thead><tr><th scope="col">ID</th><th scope="col">Zadanie</th><th scope="col">Status</th><th scope="col">Utworzono</th></tr></thead><tbody>{recent(jobs ?? []).slice(0, 5).map((job) => { const state = jobState(job.status); return <tr key={job.id}><td><Link to={`/jobs?${new URLSearchParams({ job: job.id })}`} title={job.id}>{job.id.slice(0, 8)}</Link></td><td className="clip-cell" title={job.type}>{job.type}</td><td><span className={`console-badge badge-${state.tone}`}><span className="status-dot" />{state.label}</span></td><td title={time(job.created_at, true)}>{time(job.created_at).slice(0, 5)}</td></tr> })}
          {!jobs?.length && <tr><td colSpan={4} className="console-empty">{jobs === null || jobs === undefined ? 'Brak odczytu zadań.' : 'Brak zadań w historii.'}</td></tr>}
        </tbody></table></div>
      </section>
      <LogPreview />
      <section className="console-panel" id="host-monitoring"><PanelHeading title="Monitoring" icon="activity" /><MetricHistory history={history} error={monitoring.error} /></section>
    </div>
    <div className="console-update" role="status">{overview.loading ? 'Aktualizacja danych…' : overview.updatedAt ? `Ostatni odczyt: ${time(overview.updatedAt)}` : 'Oczekiwanie na API'}<span>Inwentarz co 30 s · zasoby co 10 s</span></div>
    {warnings.length > 0 && <section className="console-warnings" aria-label="Problemy z odczytem"><h2>Wymagają uwagi</h2>{warnings.map((warning, index) => <p className="error-banner" role="alert" key={`${index}:${warning}`}>{warning}</p>)}</section>}
  </div>
}

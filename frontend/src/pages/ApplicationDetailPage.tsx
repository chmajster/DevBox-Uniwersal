import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { APPLICATION_TABS } from '../routes'
import { request } from '../api/client'
import type { Job, LogEntry } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { JobProgress } from '../components/JobProgress'
import { Modal } from '../components/Modal'
import { Icon } from '../components/Icon'
import { usePolling } from '../control-room/usePolling'
import { ApplicationStatus, ConfigurationFields } from '../applications/components'
import { DatabaseBinding } from '../applications/DatabaseBinding'
import { ApplicationResources } from '../applications/ApplicationResources'
import { ApplicationMetadataForm } from '../applications/ApplicationMetadataForm'
import { ApplicationOverview } from '../applications/ApplicationOverview'
import { ApplicationRuntime, ApplicationEndpoints, ApplicationContainers } from '../applications/ApplicationTopology'
import { runtimeLabel, tabLabels, type ApplicationTab } from '../applications/detailModel'
import { date, endpointURL, message, readConfiguration, validateManagedConfiguration, sourceNames, type ApplicationDetail } from '../applications/model'
import '../applications/detail.css'

const tabs = APPLICATION_TABS
function ConfigForm({ app, disabled, onSaved }: { app: ApplicationDetail; disabled: boolean; onSaved(): void }) {
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [deploymentMode, setDeploymentMode] = useState(() => {
    if (app.source_config?.deployment_mode) return String(app.source_config.deployment_mode)
    const activeDriver = String(app.source_config?.deployment_driver ?? app.driver)
    if (activeDriver === 'compose') return 'compose'
    if (activeDriver === 'managed' || !activeDriver) return 'auto'
    return ''
  })
  const phpRuntime = app.runtime?.name.toLowerCase() === 'php' || app.source_config?.runtime === 'php'
  const moduleContainerID = app.workloads.find((workload) => workload.primary)?.driver_resource_id
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setError(''); setSaved(false)
    const fields = new FormData(event.currentTarget)
    try {
      // Preserve supported API-only options not represented by this form.
      const config = { ...app.source_config }
      if (deploymentMode) {
        for (const key of ['container_port', 'host_port', 'deployment_mode', 'runtime', 'runtime_version', 'compose_service', 'protocol', 'health_path', 'environment', 'modules', 'start_command', 'domain', 'tls_mode', 'working_directory', 'mount_target', 'restart_policy', 'document_root']) delete config[key]
        Object.assign(config, readConfiguration(fields), { deployment_mode: deploymentMode })
      } else {
        Object.assign(config, readConfiguration(fields))
      }
      validateManagedConfiguration(config)
      setSaving(true)
      const secrets = JSON.parse(String(fields.get('secret_environment') || '{}')) as Record<string, string>
      for (const [name, value] of Object.entries(secrets)) await request(`/applications/${encodeURIComponent(app.id)}/secrets/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify({ value }) })
      await request(`/applications/${encodeURIComponent(app.id)}`, { method: 'PATCH', body: JSON.stringify({ auto_start: fields.has('auto_start'), configuration: config }) })
      setSaved(true); onSaved()
    } catch (error) { setError(message(error)) } finally { setSaving(false) }
  }
  return <form onSubmit={(event) => void submit(event)}><fieldset className="acp-card" disabled={disabled || saving}><legend>Konfiguracja aplikacji</legend>
    <ConfigurationFields value={{ ...app.source_config, ...(phpRuntime && !app.source_config?.runtime ? { runtime: 'php' } : {}) }} deploymentMode={deploymentMode} hasProvisionedWorkloads={app.workloads.length > 0} applicationId={app.id} moduleContainerID={moduleContainerID} phpRuntime={phpRuntime} onDeploymentModeChange={setDeploymentMode} /><label className="acp-check"><input type="checkbox" name="auto_start" defaultChecked={app.auto_start} />Przywracanie stanu docelowego po restarcie DevBox</label>
    <p className="app-form-hint">Zmiana technologii, wersji, modułów lub katalogu publicznego uruchamia przebudowę działającej aplikacji. Pozostałe zmiany zastosuj przyciskiem „Wdróż”.</p>
    {error && <p role="alert" className="error-banner">{error}</p>}{saved && <p role="status">Konfiguracja zapisana. Postęp przebudowy sprawdzisz w Zadaniach.</p>}
    <button type="submit">{saving ? 'Zapisywanie…' : 'Zapisz konfigurację'}</button>
  </fieldset></form>
}
function ApplicationLogs({ id, workloads }: { id: string; workloads: ApplicationDetail['workloads'] }) {
  const [workload, setWorkload] = useState('')
  const [tail, setTail] = useState('200')
  const [query, setQuery] = useState('')
  const [stream, setStream] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const consoleRef = useRef<HTMLPreElement>(null)
  const load = useCallback((signal: AbortSignal) => request<{ workload: string; line: string; stream?: string }[]>(`/applications/${encodeURIComponent(id)}/logs?${new URLSearchParams({ tail, workload })}`, { signal }), [id, workload, tail])
  const logs = usePolling(load, 2000)
  const entries = (logs.data ?? []).filter((entry) => entry.line.toLowerCase().includes(query.toLowerCase()) && (!stream || entry.stream === stream))
  useEffect(() => { if (autoScroll && consoleRef.current) consoleRef.current.scrollTop = consoleRef.current.scrollHeight }, [logs.data, autoScroll])
  return <section className="acp-card"><div className="app-section-title"><Icon name="logs" /><h2>Logi kontenera</h2><span className="app-subtle-label">Aktualizacja co 2 s</span></div><div className="acp-toolbar app-log-filters">
    <label>Usługa<select value={workload} onChange={(event) => setWorkload(event.target.value)}><option value="">Wszystkie</option>{workloads.map((item) => <option key={item.id} value={item.name}>{item.name}</option>)}</select></label>
    <label>Liczba wierszy<select value={tail} onChange={(event) => setTail(event.target.value)}>{['100','200','500','2000'].map((value) => <option key={value}>{value}</option>)}</select></label>
    <label>Szukaj w logach<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Wpisz szukaną frazę…" /></label>
    <label>Strumień<select value={stream} onChange={(event) => setStream(event.target.value)}><option value="">stdout + stderr</option><option>stdout</option><option>stderr</option></select></label>
    <label className="acp-check"><input type="checkbox" checked={autoScroll} onChange={(event) => setAutoScroll(event.target.checked)} />Przewijaj do nowych wpisów</label>
    <button className="secondary-button" onClick={logs.refresh}>Odśwież</button>
  </div>{logs.error && <p className="error-banner" role="alert">{logs.error}</p>}<pre ref={consoleRef} className="acp-logs" tabIndex={0} aria-label="Logi aplikacji">{entries.map((entry) => `[${entry.workload}] [${entry.stream ?? 'stdout/stderr'}] ${entry.line}`).join('\n') || (logs.loading && !logs.data ? 'Wczytywanie logów…' : 'Brak wpisów dla wybranego filtra.')}</pre><p className="app-resource-caption">Widoczne wpisy: {entries.length} · odczyt {logs.updatedAt ? new Date(logs.updatedAt).toLocaleTimeString('pl-PL') : '—'}</p></section>
}

function Secrets({ id, disabled }: { id: string; disabled: boolean }) {
  const load = useCallback((signal: AbortSignal) => request<string[]>(`/applications/${encodeURIComponent(id)}/secrets`, { signal }), [id])
  const names = usePolling(load, 10000)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [removeName, setRemoveName] = useState('')
  const [notice, setNotice] = useState('')
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const fields = new FormData(form)
    setSaving(true); setError(''); setNotice('')
    try { await request(`/applications/${encodeURIComponent(id)}/secrets/${encodeURIComponent(String(fields.get('name')))}`, { method: 'PUT', body: JSON.stringify({ value: fields.get('value') }) }); form.reset(); names.refresh(); setNotice('Sekret zapisany. Kolejne wdrożenie wstrzyknie go do kontenerów.') }
    catch (error) { setError(message(error)) } finally { setSaving(false) }
  }
  async function remove() {
    setSaving(true); setError('')
    try { await request(`/applications/${encodeURIComponent(id)}/secrets/${encodeURIComponent(removeName)}`, { method: 'DELETE' }); names.refresh(); setRemoveName('') }
    catch (error) { setError(message(error)) } finally { setSaving(false) }
  }
  return <section className="acp-card"><h2>Sekrety aplikacji</h2><p>Wartości są szyfrowane w SecretStore i przekazywane wszystkim usługom tej aplikacji jako zmienne środowiskowe. API nie zwraca zapisanej wartości. Administrator hosta / Docker nadal ma dostęp do środowiska kontenera.</p>
    {(error || names.error) && <p className="error-banner" role="alert">{error || names.error}</p>}{notice && <p role="status">{notice}</p>}
    <div className="acp-secret-list">{names.data?.map((name) => <div key={name}><code>{name}</code><button className="secondary-button" disabled={disabled || saving} onClick={() => setRemoveName(name)}>Usuń</button></div>)}{names.data?.length === 0 && <p>Brak sekretów.</p>}</div>
    <form onSubmit={(event) => void save(event)}><fieldset disabled={disabled || saving}><legend>Dodaj lub zastąp sekret</legend><div className="acp-fields"><label>Nazwa<input name="name" required pattern="[A-Za-z_][A-Za-z0-9_]*" autoComplete="off" placeholder="DB_PASSWORD" /></label><label>Nowa wartość<input type="password" name="value" required autoComplete="new-password" /></label></div><button type="submit">Zapisz sekret</button></fieldset></form>
    <Modal open={!!removeName} onClose={() => { if (!saving) setRemoveName('') }} labelId="acp-secret-delete"><h2 id="acp-secret-delete">Usuń sekret {removeName}</h2><p>Kolejne wdrożenie nie otrzyma tej wartości. Już uruchomione kontenery zachowają swoje środowisko.</p><div className="acp-actions"><button className="secondary-button" disabled={saving} onClick={() => setRemoveName('')}>Anuluj</button><button disabled={disabled || saving} onClick={() => void remove()}>Usuń sekret</button></div></Modal>
  </section>
}
function Events({ id }: { id: string }) {
  const load = useCallback((signal: AbortSignal) => request<Record<string, unknown>[]>(`/applications/${encodeURIComponent(id)}/events`, { signal }), [id])
  const events = usePolling(load, 5000)
  return <section className="acp-card"><div className="app-section-title"><Icon name="clock" /><h2>Zdarzenia aplikacji</h2></div>{events.error && <p className="error-banner" role="alert">{events.error}</p>}{events.data?.length ? <div className="app-event-list">{events.data.map((entry, index) => <details key={String(entry.id ?? index)}><summary><Icon name="activity" size={15} /><strong>{String(entry.type ?? entry.event_type ?? 'Zdarzenie')}</strong><span>{entry.created_at ? date(String(entry.created_at)) : String(entry.stage ?? '')}</span></summary><pre className="acp-logs" tabIndex={0}>{JSON.stringify(entry, null, 2)}</pre></details>)}</div> : <p className="muted">{events.loading && !events.data ? 'Wczytywanie zdarzeń…' : 'Brak zapisanych zdarzeń.'}</p>}</section>
}
function ApplicationJobs({ id, selected }: { id: string; selected: string }) {
  const load = useCallback((signal: AbortSignal) => request<Job[]>(`/applications/${encodeURIComponent(id)}/jobs`, { signal }), [id])
  const jobs = usePolling(load, 2000)
  const [choice, setChoice] = useState('')
  const items = (jobs.data ?? []).filter((job) => job.application_id === id)
  const job = items.find((item) => item.id === (choice || selected)) ?? items[0]
  const loadLogs = useCallback((signal: AbortSignal) => job ? request<LogEntry[]>(`/jobs/${encodeURIComponent(job.id)}/logs`, { signal }) : Promise.resolve([]), [job])
  const logs = usePolling(loadLogs, 2000)
  return <section className="acp-card"><h2>Zadania</h2>{jobs.error && <p role="alert">{jobs.error}</p>}<label>Zadanie<select value={job?.id ?? ''} onChange={(event) => setChoice(event.target.value)}>{items.map((item) => <option key={item.id} value={item.id}>{item.type} · {item.status} · {date(item.created_at)}</option>)}</select></label>{job ? <><JobProgress job={job} logs={logs.data ?? []} /><p>Wynik: {job.status} · <Link to={`/jobs?job=${encodeURIComponent(job.id)}`}>Szczegóły i anulowanie</Link></p></> : <p>Brak zadań.</p>}</section>
}
export function ApplicationDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { user } = useAuth()
  const [params, setParams] = useSearchParams()
  const requestedTab = params.get('tab') as ApplicationTab
  const selectedTab = tabs.includes(requestedTab) ? requestedTab : 'Overview'
  function setTab(next: ApplicationTab) {
    const query = new URLSearchParams(params)
    if (next === 'Overview') query.delete('tab')
    else query.set('tab', next)
    setParams(query, { preventScrollReset: true })
  }
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const actionPending = useRef(false)
  const more = useRef<HTMLDetailsElement>(null)
  const tabNavigation = useRef<HTMLElement>(null)
  const [jobID, setJobID] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [confirmation, setConfirmation] = useState('')
  useEffect(() => {
    function outside(event: PointerEvent) {
      if (more.current && !more.current.contains(event.target as Node)) more.current.open = false
    }
    function escape(event: KeyboardEvent) {
      if (event.key === 'Escape' && more.current?.open) {
        more.current.open = false
        more.current.querySelector('summary')?.focus()
      }
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape) }
  }, [])
  const load = useCallback(async (signal: AbortSignal) => {
    let stateError = ''
    try { await request(`/applications/${encodeURIComponent(id)}/state`, { signal }) }
    catch (cause) { stateError = message(cause) }
    const app = await request<ApplicationDetail>(`/applications/${encodeURIComponent(id)}`, { signal })
    return { app, stateError }
  }, [id])
  const detail = usePolling(load, 5000)
  const app = detail.data?.app
  useEffect(() => {
    tabNavigation.current?.querySelector<HTMLElement>('[aria-current]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [selectedTab, app?.driver])
  const editable = user?.role === 'admin' || user?.role === 'operator'
  const locked = busy || !!app?.active_operation
  async function action(actionName: string) {
    if (!editable || locked || actionPending.current) return
    if (more.current) more.current.open = false
    actionPending.current = true
    setTab('Jobs')
    setBusy(true); setError('')
    try {
      const result = await request<Job | { job: Job }>(`/applications/${encodeURIComponent(id)}/${actionName}`, { method: 'POST' })
      setJobID('job' in result ? result.job.id : result.id)
      detail.refresh()
    } catch (cause) { setError(message(cause)) } finally { actionPending.current = false; setBusy(false) }
  }
  function openDelete() {
    if (more.current) more.current.open = false
    setConfirmation(''); setDeleting(true)
  }
  async function remove(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError('')
    try {
      const job = await request<Job>(`/applications/${encodeURIComponent(id)}`, { method: 'DELETE', body: JSON.stringify({ remove_containers: true, delete_configuration: true, remove_source: false, remove_generated_images: false, remove_volumes: false }) })
      navigate(`/jobs?job=${encodeURIComponent(job.id)}`)
    } catch (cause) { setError(message(cause)) } finally { setBusy(false) }
  }
  if (!app) return <div className="acp application-detail"><Link to="/apps">← Aplikacje</Link><section className="acp-card app-loading"><Icon name={detail.error ? 'alert' : 'box'} size={28} /><h1>{detail.error ? 'Nie udało się wczytać aplikacji' : 'Wczytywanie aplikacji…'}</h1><p role={detail.error ? 'alert' : 'status'}>{detail.error || 'Pobieranie konfiguracji i aktualnego stanu kontenerów.'}</p><button className="secondary-button" onClick={detail.refresh}><Icon name="refresh" size={16} />Spróbuj ponownie</button></section></div>
  const tab = selectedTab === 'Services' && app.driver !== 'compose' ? 'Overview' : selectedTab
  const inspectionFailed = !!detail.data?.stateError
  const primary = app.endpoints.find((endpoint) => endpoint.primary)
  const url = inspectionFailed ? null : endpointURL(primary, window.location.hostname)
  const recent = app.deployments[0]
  const provisioned = app.workloads.length > 0
  const isRunning = ['running', 'starting', 'restarting'].includes(app.observed_state)
  const visibleTabs = tabs.filter((item) => item !== 'Services' || app.driver === 'compose')
  const extraActions = [
    { action: 'build', label: 'Zbuduj obraz', hint: 'Przygotuj obraz bez zmiany kontenera', icon: 'box' as const },
    { action: 'rebuild', label: 'Przebuduj', hint: 'Zbuduj i wymień kontener aplikacji', icon: 'refresh' as const },
    { action: 'recreate', label: 'Odtwórz kontenery', hint: 'Zastosuj konfigurację z istniejącego obrazu', icon: 'backup' as const },
    ...(app.driver === 'compose' ? [
      { action: 'pull', label: 'Pobierz obrazy', hint: 'Pobierz obrazy serwisów Compose', icon: 'archive' as const },
      { action: 'down', label: 'Wyłącz stack', hint: 'Usuń kontenery, zachowaj trwałe dane', icon: 'stop' as const },
    ] : []),
  ]
  return <div className="acp application-detail">
    <header className="acp-header app-detail-header">
      <div className="app-heading-main">
        <Link className="app-back-link" to="/apps">← Wszystkie aplikacje</Link>
        <div className="app-title-row"><div className="app-runtime-mark" aria-hidden="true"><Icon name="code" size={26} /></div><div>
          <div className="app-name-row"><h1>{app.name}</h1><ApplicationStatus value={inspectionFailed ? 'unknown' : app.status} /></div>
          <p className="app-heading-meta">{runtimeLabel(app)}<span>·</span>{sourceNames[app.source_type] ?? app.source_type}</p>
        </div></div>
        {app.description && <p className="app-description">{app.description}</p>}
      </div>
      <div className="acp-actions app-heading-actions">
        {editable && <button className="secondary-button" disabled={locked} onClick={() => setTab('Settings')}><Icon name="edit" size={15} />Zmień nazwę</button>}
        {url && <a className="button-link app-open-button" href={url} target="_blank" rel="noreferrer"><Icon name="external" size={16} />Otwórz aplikację</a>}
      </div>
    </header>
    <div className="app-operation-bar">
      <div className="acp-actions">
        <button className="secondary-button" disabled={!editable || locked || !provisioned} onClick={() => void action(isRunning ? 'stop' : 'start')}><Icon name={isRunning ? 'stop' : 'play'} size={15} />{isRunning ? 'Zatrzymaj' : 'Uruchom'}</button>
        <button className="secondary-button" disabled={!editable || locked || !provisioned} onClick={() => void action('restart')}><Icon name="refresh" size={15} />Restart</button>
        <span className="app-operation-divider" />
        <button className="app-deploy-button" disabled={!editable || locked} onClick={() => void action('deploy')}><Icon name="box" size={15} />Wdróż</button>
        <details ref={more} className="app-more-actions">
          <summary><Icon name="menu" size={15} />Więcej działań<Icon name="chevron" size={13} /></summary>
          <div className="app-action-popover">
            {extraActions.map((item) => <button key={item.action} aria-label={item.label} title={item.hint} disabled={!editable || locked || !provisioned} onClick={() => void action(item.action)}><Icon name={item.icon} size={17} /><span><strong>{item.label}</strong><small>{item.hint}</small></span></button>)}
            <button aria-label="Odśwież stan" disabled={!editable || locked} onClick={() => void action('reconcile')}><Icon name="activity" size={17} /><span><strong>Odśwież stan</strong><small>Sprawdź kontenery w Dockerze</small></span></button>
            {user?.role === 'admin' && <button className="app-danger-action" aria-label="Usuń" disabled={locked} onClick={openDelete}><Icon name="trash" size={17} /><span><strong>Usuń</strong><small>Zachowaj źródła i trwałe dane</small></span></button>}
          </div>
        </details>
      </div>
      <button className="secondary-button app-read-status" onClick={detail.refresh} title="Pobierz aktualny widok"><Icon name="refresh" size={13} /><span>Odczyt {detail.updatedAt ? new Date(detail.updatedAt).toLocaleTimeString('pl-PL') : '—'}</span></button>
    </div>
    {(error || detail.error || detail.data?.stateError) && <p className="error-banner" role="alert">{error || detail.error || `Odczyt stanu nie powiódł się: ${detail.data?.stateError}`}</p>}
    {app.active_operation && <div className="app-operation-notice" role="status"><Icon name="clock" size={20} /><div><strong>Operacja w toku</strong><p>{app.active_operation.type} · {app.active_operation.status}</p></div><Link to={`/jobs?job=${encodeURIComponent(app.active_operation.id)}`}>Postęp i anulowanie<Icon name="arrow" size={15} /></Link></div>}
    {jobID && !app.active_operation && <p className="acp-notice" role="status">Zlecono zadanie. <Link to={`/jobs?job=${encodeURIComponent(jobID)}`}>Sprawdź wynik</Link>. Bieżący stan aplikacji jest odczytywany osobno.</p>}
    {recent?.status === 'waiting_for_configuration' && <p className="acp-notice">Wdrożenie wymaga konfiguracji. Uzupełnij runtime, port lub główną usługę w Ustawieniach i ponów wdrożenie. Szczegóły znajdziesz w Zadaniach.</p>}
    <nav ref={tabNavigation} className="acp-tabs app-detail-tabs" aria-label="Sekcje aplikacji">{visibleTabs.map((item) => <button id={`app-tab-${item}`} key={item} className="secondary-button" aria-current={tab === item ? 'page' : undefined} onClick={() => setTab(item)}><Icon name={tabLabels[item].icon} size={16} />{tabLabels[item].label}</button>)}</nav>
    <div className="app-tab-content" role="region" aria-labelledby={`app-tab-${tab}`}>
      {tab === 'Overview' && <><ApplicationOverview app={app} url={url} inspectionFailed={inspectionFailed} onTab={setTab} /><ApplicationResources id={app.id} /></>}
      {tab === 'Runtime' && <ApplicationRuntime app={app} editable={editable} />}
      {tab === 'Database' && <DatabaseBinding applicationId={app.id} disabled={!editable || locked} />}
      {(tab === 'Services' || tab === 'Docker') && <ApplicationContainers app={app} inspectionFailed={inspectionFailed} />}
      {(tab === 'Ports' || tab === 'Domains') && <ApplicationEndpoints app={app} inspectionFailed={inspectionFailed} domains={tab === 'Domains'} />}
      {tab === 'Settings' && <div className="app-settings-layout">
        {!editable && <p className="acp-notice">Tryb podglądu. Edycja wymaga roli Administrator lub Operator.</p>}
        <ApplicationMetadataForm key={app.id} app={app} disabled={!editable || locked} onSaved={detail.refresh} />
        <ConfigForm key={`${app.id}:${app.driver}:${String(app.source_config?.deployment_mode ?? '')}:${String(app.source_config?.deployment_driver ?? '')}`} app={app} disabled={!editable || locked} onSaved={detail.refresh} />
        {user?.role === 'admin' && <section className="acp-card app-danger-zone"><div><h2>Usuń aplikację</h2><p>Usunięcie konfiguracji i kontenerów zachowa katalog źródłowy oraz trwałe dane.</p></div><button className="secondary-button" disabled={locked} onClick={openDelete}><Icon name="trash" size={15} />Usuń aplikację</button></section>}
      </div>}
      {tab === 'Jobs' && <><ApplicationJobs id={app.id} selected={app.active_operation?.id ?? jobID} /><section className="acp-card"><div className="app-section-title"><Icon name="clock" /><h2>Historia wdrożeń</h2></div><div className="acp-table"><table><thead><tr><th>Utworzono</th><th>Wynik</th><th>Etap</th><th>Rewizja / zadanie</th><th>Błąd</th></tr></thead><tbody>{app.deployments.map((deployment) => <tr key={deployment.id}><td>{date(deployment.created_at)}</td><td><ApplicationStatus value={deployment.status} /></td><td>{deployment.stage}</td><td><code>{deployment.source_revision?.slice(0, 12) || '—'}</code>{deployment.job_id && <Link className="acp-line" to={`/jobs?job=${encodeURIComponent(deployment.job_id)}`}>Szczegóły zadania</Link>}</td><td>{deployment.error || '—'}</td></tr>)}{!app.deployments.length && <tr><td colSpan={5}>Brak wdrożeń. Historia pojawi się po pierwszej operacji.</td></tr>}</tbody></table></div></section></>}
      {tab === 'Logs' && <ApplicationLogs id={app.id} workloads={app.workloads} />}
      {tab === 'Environment' && <><section className="acp-card"><div className="app-section-title"><Icon name="code" /><h2>Zmienne środowiskowe</h2><button className="secondary-button app-section-action" disabled={!editable || locked} onClick={() => setTab('Settings')}><Icon name="edit" size={15} />Edytuj zmienne</button></div>{Object.keys((app.source_config?.environment ?? {}) as Record<string, string>).length ? <dl className="app-environment-values">{Object.entries((app.source_config?.environment ?? {}) as Record<string, string>).map(([name, value]) => <div key={name}><dt><code>{name}</code></dt><dd><code>{value}</code></dd></div>)}</dl> : <p className="muted">Nie dodano zwykłych zmiennych środowiskowych. Wartości i import .env znajdziesz w Ustawieniach.</p>}</section><Secrets id={app.id} disabled={!editable || locked} /></>}
      {tab === 'Docker' && <><ApplicationResources id={app.id} /><Events id={app.id} /></>}
    </div>
    <Modal open={deleting} onClose={() => { if (!busy) setDeleting(false) }} labelId="acp-delete-title"><h2 id="acp-delete-title">Usuń aplikację</h2><p>Kontenery, sekrety i konfiguracja „{app.name}” zostaną usunięte. Katalog źródłowy, obrazy i trwałe wolumeny zostaną zachowane.</p><form onSubmit={(event) => void remove(event)}><label>Wpisz nazwę aplikacji<input value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="off" /></label>{error && <p role="alert">{error}</p>}<div className="acp-actions"><button type="button" className="secondary-button" disabled={busy} onClick={() => setDeleting(false)}>Anuluj</button><button type="submit" disabled={confirmation !== app.name || locked}>Usuń aplikację</button></div></form></Modal>
  </div>
}

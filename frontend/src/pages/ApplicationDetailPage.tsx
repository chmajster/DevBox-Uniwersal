import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { APPLICATION_TABS } from '../routes'
import { apiURL, request } from '../api/client'
import type { Job, LogEntry } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { JobProgress } from '../components/JobProgress'
import { Modal } from '../components/Modal'
import { usePolling } from '../control-room/usePolling'
import { ApplicationStatus, ConfigurationFields } from '../applications/components'
import { DatabaseBinding } from '../applications/DatabaseBinding'
import { ApplicationResources } from '../applications/ApplicationResources'
import { date, deploymentModeName, endpointURL, message, readConfiguration, validateManagedConfiguration, sourceNames, type ApplicationDetail } from '../applications/model'

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
      await request(`/applications/${encodeURIComponent(app.id)}`, { method: 'PATCH', body: JSON.stringify({ name: fields.get('name'), description: fields.get('description'), auto_start: fields.has('auto_start'), configuration: config }) })
      setSaved(true); onSaved()
    } catch (error) { setError(message(error)) } finally { setSaving(false) }
  }
  return <form onSubmit={(event) => void submit(event)}><fieldset className="acp-card" disabled={disabled || saving}><legend>Konfiguracja aplikacji</legend>
    <div className="acp-fields"><label>Nazwa<input name="name" required defaultValue={app.name} /></label><label>Opis<input name="description" defaultValue={app.description} /></label></div>
    <ConfigurationFields value={{ ...app.source_config, ...(phpRuntime && !app.source_config?.runtime ? { runtime: 'php' } : {}) }} deploymentMode={deploymentMode} hasProvisionedWorkloads={app.workloads.length > 0} applicationId={app.id} moduleContainerID={moduleContainerID} phpRuntime={phpRuntime} onDeploymentModeChange={setDeploymentMode} /><label className="acp-check"><input type="checkbox" name="auto_start" defaultChecked={app.auto_start} />Przywracanie stanu docelowego po restarcie DevBox</label>
    <p>Zmiana technologii lub wersji uruchamia przebudowę działającej aplikacji. Pozostałe zmiany zastosuj przyciskiem „Deploy”.</p>
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
  return <section className="acp-card"><div className="acp-toolbar">
    <label>Usługa<select value={workload} onChange={(event) => setWorkload(event.target.value)}><option value="">Wszystkie</option>{workloads.map((item) => <option key={item.id} value={item.name}>{item.name}</option>)}</select></label>
    <label>Tail<select value={tail} onChange={(event) => setTail(event.target.value)}>{['100','200','500','2000'].map((value) => <option key={value}>{value}</option>)}</select></label>
    <label>Szukaj / filtruj<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} /></label>
    <label>Strumień<select value={stream} onChange={(event) => setStream(event.target.value)}><option value="">stdout + stderr</option><option>stdout</option><option>stderr</option></select></label>
    <label className="acp-check"><input type="checkbox" checked={autoScroll} onChange={(event) => setAutoScroll(event.target.checked)} />Auto-scroll</label>
    <button className="secondary-button" onClick={logs.refresh}>Odśwież</button>
  </div>{logs.error && <p role="alert">{logs.error}</p>}<pre ref={consoleRef} className="acp-logs" tabIndex={0} aria-label="Logi aplikacji">{entries.map((entry) => `[${entry.workload}] [${entry.stream ?? 'stdout/stderr'}] ${entry.line}`).join('\n') || 'Brak wpisów.'}</pre></section>
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
  return <section className="acp-card"><h2>Zdarzenia</h2>{events.error && <p role="alert">{events.error}</p>}<pre className="acp-logs" tabIndex={0}>{events.data?.length ? events.data.map((entry) => JSON.stringify(entry, null, 2)).join('\n') : 'Brak zdarzeń.'}</pre></section>
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
  const [params] = useSearchParams()
  const [tab, setTab] = useState<typeof tabs[number]>(() => tabs.includes(params.get('tab') as typeof tabs[number]) ? params.get('tab') as typeof tabs[number] : 'Overview')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const actionPending = useRef(false)
  const [jobID, setJobID] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [confirmation, setConfirmation] = useState('')
  const load = useCallback(async (signal: AbortSignal) => {
    // A failed provider inspection must not keep showing a stale green state.
    let stateError = ''
    try { await request(`/applications/${encodeURIComponent(id)}/state`, { signal }) }
    catch (error) { stateError = message(error) }
    const app = await request<ApplicationDetail>(`/applications/${encodeURIComponent(id)}`, { signal })
    return { app, stateError }
  }, [id])
  const detail = usePolling(load, 5000)
  const app = detail.data?.app
  const editable = user?.role === 'admin' || user?.role === 'operator'
  const locked = busy || !!app?.active_operation
  async function action(action: string) {
    if (!editable || locked || actionPending.current) return
    actionPending.current = true
    setTab('Jobs')
    setBusy(true); setError('')
    try { const result = await request<Job | { job: Job }>(`/applications/${encodeURIComponent(id)}/${action}`, { method: 'POST' }); setJobID('job' in result ? result.job.id : result.id); detail.refresh() }
    catch (error) { setError(message(error)) } finally { actionPending.current = false; setBusy(false) }
  }
  async function remove(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError('')
    try { const job = await request<Job>(`/applications/${encodeURIComponent(id)}`, { method: 'DELETE', body: JSON.stringify({ remove_containers: true, delete_configuration: true, remove_source: false, remove_generated_images: false, remove_volumes: false }) }); navigate(`/jobs?job=${encodeURIComponent(job.id)}`) }
    catch (error) { setError(message(error)) } finally { setBusy(false) }
  }
  if (!app) return <div className="acp"><Link to="/apps">Aplikacje</Link><p role={detail.error ? 'alert' : 'status'}>{detail.error || 'Wczytywanie aplikacji…'}</p><button onClick={detail.refresh}>Odśwież</button></div>
  const primary = app.endpoints.find((endpoint) => endpoint.primary)
  const url = endpointURL(primary, window.location.hostname)
  const recent = app.deployments[0]
  const runtimeMetadata = app.runtime?.metadata ?? {}
  const volumes = Array.isArray(runtimeMetadata.volumes) ? runtimeMetadata.volumes as { type?: string; source?: string; target?: string; read_only?: boolean }[] : []
  const profile = String(runtimeMetadata.profile ?? recent?.plan_snapshot?.runtime?.metadata?.profile ?? app.runtime?.name ?? '')
  const profileNames: Record<string, string> = { wordpress: 'WordPress', generic_php: 'PHP', generic_node: 'Node.js', generic_python: 'Python', generic_go: 'Go', generic_static: 'Static', php: 'PHP', node: 'Node.js', python: 'Python', go: 'Go', static: 'Static' }
  const runtimeSource = String(app.source.docker_image || runtimeMetadata.source_path || app.source.local_path || app.source.repository_url || app.slug)
  const runtimeGenerated = runtimeMetadata.generated === true
  return <div className="acp"><header className="acp-header"><div><Link to="/apps">Aplikacje</Link><h1>{app.name}</h1><p>{app.description || sourceNames[app.source_type]}</p></div><div className="acp-actions"><ApplicationStatus value={detail.data?.stateError ? 'unknown' : app.status} />{url && <a className="button-link secondary-button" href={url} target="_blank" rel="noreferrer">Otwórz aplikację ↗</a>}<button disabled={!editable || locked} onClick={() => void action('deploy')}>Deploy</button></div></header>
    {(error || detail.error || detail.data?.stateError) && <p className="error-banner" role="alert">{error || detail.error || `Odczyt stanu nie powiódł się: ${detail.data?.stateError}`}</p>}
    <div className="acp-toolbar"><span>Odczyt: {detail.updatedAt ? new Date(detail.updatedAt).toLocaleTimeString('pl-PL') : '—'}</span><div className="acp-actions">{['start', 'stop', 'restart', 'build', 'rebuild', 'recreate', ...(app.driver === 'compose' ? ['pull', 'down'] : [])].map((actionName, i) => <button className="secondary-button" key={actionName} disabled={!editable || locked || !app.workloads.length} onClick={() => void action(actionName)}>{['Start', 'Stop', 'Restart', 'Build', 'Rebuild', 'Recreate', 'Pull', 'Down'][i]}</button>)}<button className="secondary-button" disabled={!editable || locked} title="Odczytaj rzeczywisty stan kontenerów" onClick={() => void action('reconcile')}>Odśwież stan</button>{user?.role === 'admin' && <button className="secondary-button" disabled={locked} onClick={() => { setConfirmation(''); setDeleting(true) }}>Usuń</button>}</div></div>
    {app.active_operation && <p className="acp-notice" role="status">Operacja: {app.active_operation.type} · {app.active_operation.status}. <Link to={`/jobs?job=${encodeURIComponent(app.active_operation.id)}`}>Postęp i anulowanie</Link></p>}
    {jobID && !app.active_operation && <p role="status">Zlecone zadanie: <Link to={`/jobs?job=${encodeURIComponent(jobID)}`}>{jobID.slice(0, 8)}</Link>. Stan aplikacji jest odczytywany niezależnie od wyniku zadania.</p>}
    {recent?.status === 'waiting_for_configuration' && <p className="acp-notice">{app.source_config?.deployment_mode === 'compose' ? 'Docker Compose nie wskazuje jednoznacznej usługi HTTP i portu. Ustaw główny serwis oraz port kontenera, zapisz konfigurację i ponów wdrożenie.' : 'Wdrożenie wymaga konfiguracji runtime, endpointu lub sekretów. Uzupełnij brakujące ustawienia, zapisz i ponów wdrożenie.'} Szczegóły są w zdarzeniach i wyniku zadania.</p>}
    <nav className="acp-tabs" aria-label="Sekcje aplikacji">{tabs.filter((item) => item !== 'Services' || app.driver === 'compose').map((item) => <button key={item} className="secondary-button" aria-current={tab === item ? 'page' : undefined} onClick={() => setTab(item)}>{item}</button>)}</nav>
    {tab === 'Overview' && <section className="acp-card"><h2>Stan i źródło</h2><dl className="acp-facts"><div><dt>Stan docelowy</dt><dd>{app.desired_state}</dd></div><div><dt>Stan zaobserwowany</dt><dd>{detail.data?.stateError ? 'unknown' : app.observed_state}</dd></div><div><dt>Health</dt><dd><ApplicationStatus value={detail.data?.stateError ? 'unknown' : app.health_state} /></dd></div><div><dt>Tryb wdrożenia</dt><dd>{deploymentModeName(app.source_config?.deployment_mode, app.driver)}</dd></div><div><dt>Runtime</dt><dd>{app.runtime ? `${app.runtime.name} ${app.runtime.version ?? ''}` : '—'}</dd></div><div><dt>Źródło</dt><dd>{app.source.repository_url || app.source.local_path || app.slug}</dd></div></dl></section>}
      {(tab === 'Overview' || tab === 'Runtime') && <section className="acp-card"><h2>Środowisko uruchomieniowe</h2><dl className="acp-facts">
        <div><dt>Tryb</dt><dd>{deploymentModeName(app.source_config?.deployment_mode, app.driver)}</dd></div>
        <div><dt>Wykryty typ</dt><dd>{(profileNames[profile] ?? profile) || '—'}</dd></div>
        <div><dt>Runtime</dt><dd>{app.runtime ? `${app.runtime.name} ${app.runtime.version ?? ''}` : '—'}</dd></div>
        <div><dt>Kontener</dt><dd><code>{String(runtimeMetadata.container_name ?? app.workloads.find((workload) => workload.primary)?.driver_resource_id ?? '—')}</code></dd></div>
        <div><dt>Źródło</dt><dd><code>{runtimeSource}</code></dd></div>
        <div><dt>Port kontenera</dt><dd>{primary?.container_port ?? String(runtimeMetadata.container_port ?? '—')}</dd></div>
        <div><dt>Port hosta</dt><dd>{primary?.host_port ?? '—'}</dd></div>
        <div><dt>Start command</dt><dd><code>{String(app.source_config?.start_command ?? 'Domyślna komenda runtime')}</code></dd></div><div><dt>Sieć</dt><dd>{String(runtimeMetadata.network ?? '') || '—'}</dd></div>
        <div><dt>Wygenerowany runtime</dt><dd>{runtimeGenerated ? 'Tak' : 'Nie'}</dd></div>
      </dl>
      {volumes.length > 0 && <div className="acp-table"><table><thead><tr><th>Typ</th><th>Źródło</th><th>Cel</th><th>Dostęp</th></tr></thead><tbody>{volumes.map((volume) => <tr key={`${volume.source ?? ''}:${volume.target ?? ''}`}><td>{volume.type ?? '—'}</td><td><code>{volume.source || '—'}</code></td><td><code>{volume.target ?? '—'}</code></td><td>{volume.read_only ? 'Tylko odczyt' : 'Odczyt i zapis'}</td></tr>)}</tbody></table></div>}
      <DatabaseBinding applicationId={app.id} disabled={!editable || locked} />
      {editable && app.driver === 'managed' && <a className="button-link secondary-button" href={apiURL(`/applications/${encodeURIComponent(app.id)}/export`)} download>Eksport Dockerfile i Compose</a>}
      </section>}
      {(tab === 'Services' || tab === 'Docker') && <section className="acp-card"><h2>Usługi ({app.workloads.length})</h2><div className="acp-table"><table><thead><tr><th>Nazwa</th><th>Rola</th><th>Stan</th><th>Health</th><th>Obraz / kontener</th></tr></thead><tbody>{app.workloads.map((workload) => <tr key={workload.id}><td>{workload.name}{workload.primary && <small className="acp-line">Główna</small>}</td><td>{workload.role}</td><td><ApplicationStatus value={detail.data?.stateError ? 'unknown' : workload.observed_state} /></td><td><ApplicationStatus value={detail.data?.stateError ? 'unknown' : workload.health_state} /></td><td>{workload.image || '—'}<small className="acp-line">{workload.driver_resource_id || 'Brak kontenera'}</small></td></tr>)}{!app.workloads.length && <tr><td colSpan={5}>Brak usług. Uruchom pierwsze wdrożenie.</td></tr>}</tbody></table></div></section>}
      {['Overview', 'Ports', 'Domains'].includes(tab) && <section className="acp-card"><h2>Endpointy</h2><p>Domena kieruje przez reverse proxy na opublikowany port. Dla domen lokalnych ustaw DNS lub wpis w pliku hosts na adres serwera. TLS kończy się w proxy.</p><div className="acp-table"><table><thead><tr><th>Nazwa</th><th>Protokół</th><th>Port wewnętrzny</th><th>Port hosta</th><th>Adres</th></tr></thead><tbody>{app.endpoints.map((endpoint) => { const link = endpointURL(endpoint, window.location.hostname); return <tr key={endpoint.id}><td>{endpoint.name}{endpoint.primary ? ' · główny' : ''}</td><td>{endpoint.protocol}</td><td>{endpoint.container_port}</td><td>{endpoint.host_port ?? '—'}</td><td>{link ? <a href={link} target="_blank" rel="noreferrer">{link}</a> : 'Brak adresu HTTP'}</td></tr> })}{!app.endpoints.length && <tr><td colSpan={5}>Brak endpointów.</td></tr>}</tbody></table></div></section>}
    {tab === 'Settings' && <ConfigForm key={`${app.id}:${app.driver}:${String(app.source_config?.deployment_mode ?? '')}:${Object.prototype.hasOwnProperty.call(app.source_config ?? {}, 'deployment_driver') ? String(app.source_config?.deployment_driver ?? '') : 'active'}`} app={app} disabled={!editable || locked} onSaved={detail.refresh} />}
    {tab === 'Jobs' && <><ApplicationJobs id={app.id} selected={app.active_operation?.id ?? jobID} /><section className="acp-card"><h2>Historia wdrożeń</h2><div className="acp-table"><table><thead><tr><th>Utworzono</th><th>Wynik</th><th>Etap</th><th>Rewizja / zadanie</th><th>Błąd</th></tr></thead><tbody>{app.deployments.map((deployment) => <tr key={deployment.id}><td>{date(deployment.created_at)}</td><td><ApplicationStatus value={deployment.status} /></td><td>{deployment.stage}</td><td><code>{deployment.source_revision?.slice(0, 12) || '—'}</code>{deployment.job_id && <Link className="acp-line" to={`/jobs?job=${encodeURIComponent(deployment.job_id)}`}>Zadanie, logi i retry</Link>}</td><td>{deployment.error || '—'}</td></tr>)}{!app.deployments.length && <tr><td colSpan={5}>Brak wdrożeń.</td></tr>}</tbody></table></div></section></>}
    {tab === 'Logs' && <ApplicationLogs id={app.id} workloads={app.workloads} />}
    {tab === 'Environment' && <><section className="acp-card"><h2>Environment variables</h2><dl className="acp-facts">{Object.entries((app.source_config?.environment ?? {}) as Record<string, string>).map(([name, value]) => <div key={name}><dt><code>{name}</code></dt><dd>{value}</dd></div>)}</dl><p>Edytuj zwykłe wartości i importuj .env w <button className="secondary-button" onClick={() => setTab('Settings')}>Settings</button>.</p></section><Secrets id={app.id} disabled={!editable || locked} /></>}
    {(tab === 'Overview' || tab === 'Docker') && <ApplicationResources id={app.id} />}
    {tab === 'Docker' && <Events id={app.id} />}
    <Modal open={deleting} onClose={() => { if (!busy) setDeleting(false) }} labelId="acp-delete-title"><h2 id="acp-delete-title">Usuń aplikację</h2><p>Kontenery, sekrety i konfiguracja „{app.name}” zostaną usunięte. Katalog źródłowy, obrazy i trwałe wolumeny zostaną zachowane.</p><form onSubmit={(event) => void remove(event)}><label>Wpisz nazwę aplikacji<input value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="off" /></label>{error && <p role="alert">{error}</p>}<div className="acp-actions"><button type="button" className="secondary-button" disabled={busy} onClick={() => setDeleting(false)}>Anuluj</button><button type="submit" disabled={confirmation !== app.name || locked}>Usuń aplikację</button></div></form></Modal>
  </div>
}

import { useCallback, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { request } from '../api/client'
import type { Job } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Modal } from '../components/Modal'
import { usePolling } from '../control-room/usePolling'
import { ApplicationStatus, ConfigurationFields } from '../applications/components'
import { date, endpointURL, message, readConfiguration, sourceNames, type ApplicationDetail } from '../applications/model'

const tabs = ['Przegląd', 'Konfiguracja', 'Wdrożenia', 'Logi', 'Sekrety', 'Zdarzenia'] as const
function ConfigForm({ app, disabled, onSaved }: { app: ApplicationDetail; disabled: boolean; onSaved(): void }) {
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [driver, setDriver] = useState(() => Object.prototype.hasOwnProperty.call(app.source_config ?? {}, 'deployment_driver') ? String(app.source_config?.deployment_driver ?? '') : app.driver)
  const phpRuntime = app.runtime?.name.toLowerCase() === 'php' || app.source_config?.runtime === 'php'
  const moduleContainerID = app.workloads.find((workload) => workload.primary)?.driver_resource_id
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setError(''); setSaved(false)
    const fields = new FormData(event.currentTarget)
    try {
      // Preserve supported API-only options not represented by this form.
      const config = { ...app.source_config }
      for (const key of ['container_port', 'host_port', 'runtime', 'runtime_version', 'compose_service', 'protocol', 'health_path', 'environment', 'modules']) delete config[key]
      Object.assign(config, readConfiguration(fields))
      setSaving(true)
      await request(`/applications/${encodeURIComponent(app.id)}`, { method: 'PATCH', body: JSON.stringify({ name: fields.get('name'), description: fields.get('description'), auto_start: fields.has('auto_start'), driver, configuration: config }) })
      setSaved(true); onSaved()
    } catch (error) { setError(message(error)) } finally { setSaving(false) }
  }
  return <form onSubmit={(event) => void submit(event)}><fieldset className="acp-card" disabled={disabled || saving}><legend>Konfiguracja aplikacji</legend>
    <div className="acp-fields"><label>Nazwa<input name="name" required defaultValue={app.name} /></label><label>Opis<input name="description" defaultValue={app.description} /></label></div>
    <ConfigurationFields value={{ ...app.source_config, ...(phpRuntime && !app.source_config?.runtime ? { runtime: 'php' } : {}) }} driver={driver} hasProvisionedWorkloads={app.workloads.length > 0} applicationId={app.id} moduleContainerID={moduleContainerID} phpRuntime={phpRuntime} onDriverChange={setDriver} /><label className="acp-check"><input type="checkbox" name="auto_start" defaultChecked={app.auto_start} />Przywracanie stanu docelowego po restarcie DevBox</label>
    <p>Zmiany parametrów kontenera, modułów, sterownika i sekretów wymagają kolejnego wdrożenia. Zapis nie restartuje działających usług.</p>
    {error && <p role="alert" className="error-banner">{error}</p>}{saved && <p role="status">Konfiguracja zapisana. Uruchom „Deploy”, aby ją zastosować.</p>}
    <button type="submit">{saving ? 'Zapisywanie…' : 'Zapisz konfigurację'}</button>
  </fieldset></form>
}
function ApplicationLogs({ id, workloads }: { id: string; workloads: ApplicationDetail['workloads'] }) {
  const [workload, setWorkload] = useState('')
  const load = useCallback((signal: AbortSignal) => request<{ workload: string; line: string }[]>(`/applications/${encodeURIComponent(id)}/logs?${new URLSearchParams({ tail: '200', workload })}`, { signal }), [id, workload])
  const logs = usePolling(load, 4000)
  return <section className="acp-card"><div className="acp-toolbar"><label>Usługa<select value={workload} onChange={(event) => setWorkload(event.target.value)}><option value="">Wszystkie usługi</option>{workloads.map((item) => <option key={item.id} value={item.name}>{item.name}</option>)}</select></label><button className="secondary-button" onClick={logs.refresh}>Odśwież logi</button></div>{logs.error && <p role="alert">{logs.error}</p>}<pre className="acp-logs" tabIndex={0} aria-label="Logi aplikacji">{logs.data?.map((entry) => `[${entry.workload}] ${entry.line}`).join('\n') || (logs.loading ? 'Wczytywanie…' : 'Brak wpisów.')}</pre></section>
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
export function ApplicationDetailPage() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { user } = useAuth()
  const [tab, setTab] = useState<typeof tabs[number]>('Przegląd')
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
    if (action === 'deploy') setTab('Wdrożenia')
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
  return <div className="acp"><header className="acp-header"><div><Link to="/apps">Aplikacje</Link><h1>{app.name}</h1><p>{app.description || sourceNames[app.source_type]}</p></div><div className="acp-actions"><ApplicationStatus value={detail.data?.stateError ? 'unknown' : app.status} />{url && <a className="button-link secondary-button" href={url} target="_blank" rel="noreferrer">Otwórz aplikację ↗</a>}<button disabled={!editable || locked} onClick={() => void action('deploy')}>Deploy</button></div></header>
    {(error || detail.error || detail.data?.stateError) && <p className="error-banner" role="alert">{error || detail.error || `Odczyt stanu nie powiódł się: ${detail.data?.stateError}`}</p>}
    <div className="acp-toolbar"><span>Odczyt: {detail.updatedAt ? new Date(detail.updatedAt).toLocaleTimeString('pl-PL') : '—'}</span><div className="acp-actions">{['start', 'stop', 'restart'].map((actionName, i) => <button className="secondary-button" key={actionName} disabled={!editable || locked || !app.workloads.length} onClick={() => void action(actionName)}>{['Uruchom', 'Zatrzymaj', 'Restart'][i]}</button>)}<button className="secondary-button" disabled={!editable || locked} title="Ponownie wdróż aplikację i odtwórz jej kontenery" onClick={() => void action('deploy')}>Odśwież stan</button>{user?.role === 'admin' && <button className="secondary-button" disabled={locked} onClick={() => { setConfirmation(''); setDeleting(true) }}>Usuń</button>}</div></div>
    {app.active_operation && <p className="acp-notice" role="status">Operacja: {app.active_operation.type} · {app.active_operation.status}. <Link to={`/jobs?job=${encodeURIComponent(app.active_operation.id)}`}>Postęp i anulowanie</Link></p>}
    {jobID && !app.active_operation && <p role="status">Zlecone zadanie: <Link to={`/jobs?job=${encodeURIComponent(jobID)}`}>{jobID.slice(0, 8)}</Link>. Stan aplikacji jest odczytywany niezależnie od wyniku zadania.</p>}
    {recent?.status === 'waiting_for_configuration' && <p className="acp-notice">Wdrożenie wymaga konfiguracji. Ustaw serwis / port lub wymagane sekrety, zapisz i ponownie uruchom Deploy. Szczegóły są w zdarzeniach i wyniku zadania.</p>}
    <nav className="acp-tabs" aria-label="Sekcje aplikacji">{tabs.map((item) => <button key={item} className="secondary-button" aria-current={tab === item ? 'page' : undefined} onClick={() => setTab(item)}>{item}</button>)}</nav>
    {tab === 'Przegląd' && <><section className="acp-card"><h2>Stan i źródło</h2><dl className="acp-facts"><div><dt>Stan docelowy</dt><dd>{app.desired_state}</dd></div><div><dt>Stan zaobserwowany</dt><dd>{detail.data?.stateError ? 'unknown' : app.observed_state}</dd></div><div><dt>Health</dt><dd><ApplicationStatus value={detail.data?.stateError ? 'unknown' : app.health_state} /></dd></div><div><dt>Sterownik</dt><dd>{app.driver || 'Automatycznie'}</dd></div><div><dt>Runtime</dt><dd>{app.runtime ? `${app.runtime.name} ${app.runtime.version ?? ''}` : '—'}</dd></div><div><dt>Źródło</dt><dd>{app.source.repository_url || app.source.local_path || app.source.docker_image || app.slug}</dd></div></dl></section>
      <section className="acp-card"><h2>Usługi ({app.workloads.length})</h2><div className="acp-table"><table><thead><tr><th>Nazwa</th><th>Rola</th><th>Stan</th><th>Health</th><th>Obraz / kontener</th></tr></thead><tbody>{app.workloads.map((workload) => <tr key={workload.id}><td>{workload.name}{workload.primary && <small className="acp-line">Główna</small>}</td><td>{workload.role}</td><td><ApplicationStatus value={detail.data?.stateError ? 'unknown' : workload.observed_state} /></td><td><ApplicationStatus value={detail.data?.stateError ? 'unknown' : workload.health_state} /></td><td>{workload.image || '—'}<small className="acp-line">{workload.driver_resource_id || 'Brak kontenera'}</small></td></tr>)}{!app.workloads.length && <tr><td colSpan={5}>Brak usług. Uruchom pierwsze wdrożenie.</td></tr>}</tbody></table></div></section>
      <section className="acp-card"><h2>Endpointy</h2><p>Linki wykorzystują host panelu i rzeczywiście opublikowany port. DNS, certyfikat oraz routing domeny nie są tworzone automatycznie.</p><div className="acp-table"><table><thead><tr><th>Nazwa</th><th>Protokół</th><th>Port wewnętrzny</th><th>Port hosta</th><th>Adres</th></tr></thead><tbody>{app.endpoints.map((endpoint) => { const link = endpointURL(endpoint, window.location.hostname); return <tr key={endpoint.id}><td>{endpoint.name}{endpoint.primary ? ' · główny' : ''}</td><td>{endpoint.protocol}</td><td>{endpoint.container_port}</td><td>{endpoint.host_port ?? '—'}</td><td>{link ? <a href={link} target="_blank" rel="noreferrer">{link}</a> : 'Brak adresu HTTP'}</td></tr> })}{!app.endpoints.length && <tr><td colSpan={5}>Brak endpointów.</td></tr>}</tbody></table></div></section></>}
    {tab === 'Konfiguracja' && <ConfigForm key={`${app.id}:${app.driver}:${Object.prototype.hasOwnProperty.call(app.source_config ?? {}, 'deployment_driver') ? String(app.source_config?.deployment_driver ?? '') : 'active'}`} app={app} disabled={!editable || locked} onSaved={detail.refresh} />}
    {tab === 'Wdrożenia' && <section className="acp-card"><h2>Historia wdrożeń</h2><div className="acp-table"><table><thead><tr><th>Utworzono</th><th>Wynik</th><th>Etap</th><th>Rewizja / zadanie</th><th>Błąd</th></tr></thead><tbody>{app.deployments.map((deployment) => <tr key={deployment.id}><td>{date(deployment.created_at)}</td><td><ApplicationStatus value={deployment.status} /></td><td>{deployment.stage}</td><td><code>{deployment.source_revision?.slice(0, 12) || '—'}</code>{deployment.job_id && <Link className="acp-line" to={`/jobs?job=${encodeURIComponent(deployment.job_id)}`}>Zadanie, logi i retry</Link>}</td><td>{deployment.error || '—'}</td></tr>)}{!app.deployments.length && <tr><td colSpan={5}>Brak wdrożeń.</td></tr>}</tbody></table></div></section>}
    {tab === 'Logi' && <ApplicationLogs id={app.id} workloads={app.workloads} />}
    {tab === 'Sekrety' && <Secrets id={app.id} disabled={!editable || locked} />}
    {tab === 'Zdarzenia' && <Events id={app.id} />}
    <Modal open={deleting} onClose={() => { if (!busy) setDeleting(false) }} labelId="acp-delete-title"><h2 id="acp-delete-title">Usuń aplikację</h2><p>Kontenery, sekrety i konfiguracja „{app.name}” zostaną usunięte. Katalog źródłowy, obrazy i trwałe wolumeny zostaną zachowane.</p><form onSubmit={(event) => void remove(event)}><label>Wpisz nazwę aplikacji<input value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoComplete="off" /></label>{error && <p role="alert">{error}</p>}<div className="acp-actions"><button type="button" className="secondary-button" disabled={busy} onClick={() => setDeleting(false)}>Anuluj</button><button type="submit" disabled={confirmation !== app.name || locked}>Usuń aplikację</button></div></form></Modal>
  </div>
}

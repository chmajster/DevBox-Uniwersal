import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { request } from '../api/client'
import type { Job } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { ConfigurationFields, DetectionResult } from '../applications/components'
import { DirectoryPicker } from '../components/DirectoryPicker'
import { message, readConfiguration, validateManagedConfiguration, sourceNames, type ApplicationDetail, type Detection } from '../applications/model'
import { applyDatabaseChoice, DatabaseConfiguration, emptyDatabaseChoice, waitForJob } from '../applications/databaseConfiguration'

const stages = ['Źródło', 'Technologia', 'Uruchamianie', 'Baza danych', 'Podsumowanie']
export function ApplicationWizardPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const form = useRef<HTMLFormElement>(null)
  const [step, setStep] = useState(0)
  const [sourceType, setSourceType] = useState('local')
  const [path, setPath] = useState('')
  const [directoryOpen, setDirectoryOpen] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState('')
  const [detecting, setDetecting] = useState(false)
  const [mode, setMode] = useState('auto')
  const [detection, setDetection] = useState<Detection | null>(null)
  const [createdID, setCreatedID] = useState('')
  const [databaseApplied, setDatabaseApplied] = useState(false)
  const [database, setDatabase] = useState(emptyDatabaseChoice)
  const [summary, setSummary] = useState<Record<string, unknown>>({})
  useEffect(() => {
    if (sourceType !== 'local' || !path.trim()) return
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      setDetecting(true)
      request<{ detection: Detection }>('/applications/detect', { method: 'POST', signal: controller.signal, body: JSON.stringify({ source_type: 'local', source: { local_path: path }, configuration: {} }) })
        .then(({ detection }) => { setDetection(detection); setMode(detection.compose_found ? 'compose' : detection.dockerfile_found ? 'dockerfile' : 'auto'); setError('') })
        .catch((error) => { if (!controller.signal.aborted) setError(message(error)) })
        .finally(() => { if (!controller.signal.aborted) setDetecting(false) })
    }, 500)
    return () => { window.clearTimeout(timer); controller.abort() }
  }, [path, sourceType])
  const source = (fields: FormData) => sourceType === 'local' ? { local_path: path } : sourceType === 'git' ? { repository_url: String(fields.get('repository_url') ?? ''), reference: String(fields.get('reference') ?? ''), ...(fields.get('credential_id') ? { credential_id: String(fields.get('credential_id')) } : {}) } : sourceType === 'docker_image' ? { docker_image: String(fields.get('docker_image') ?? '') } : {}
  function captureSummary() {
    if (!form.current) return
    const fields = new FormData(form.current)
    setSummary({ ...readConfiguration(fields), source_description: path || String(fields.get('repository_url') || fields.get('docker_image') || 'Katalog nowej aplikacji'), runtime_image: String(fields.get('runtime_image') || fields.get('docker_image') || '') })
  }
  async function analyzeGit() {
    if (!form.current) return
    setDetecting(true); setError('')
    try {
      const response = await request<{ job: Job }>('/applications/detect', { method: 'POST', body: JSON.stringify({ source_type: 'git', source: source(new FormData(form.current)), configuration: {} }) })
      const job = await waitForJob(response.job.id, setProgress)
      const detected = (job.result as { detection?: Detection } | undefined)?.detection
      if (detected) { setDetection(detected); setMode(detected.compose_found ? 'compose' : detected.dockerfile_found ? 'dockerfile' : 'auto') }
    } catch (cause) { setError(message(cause)) } finally { setDetecting(false); setProgress('') }
  }
  function next() {
    if (!form.current) return
    const invalid = form.current.querySelector(`[data-step="${step}"]`)?.querySelector<HTMLInputElement | HTMLSelectElement>('input:invalid, select:invalid')
    if (invalid) { invalid.reportValidity(); return }
    try { captureSummary(); setError(''); setStep(Math.min(stages.length - 1, step + 1)) } catch (cause) { setError(message(cause)) }
  }
  async function submit(event: FormEvent<HTMLFormElement>, deploy: boolean) {
    event.preventDefault()
    if (busy || detecting || !form.current) return
    const invalid = form.current.querySelector<HTMLInputElement | HTMLSelectElement>('input:invalid, select:invalid')
    if (invalid) { setStep(Number(invalid.closest('[data-step]')?.getAttribute('data-step') ?? 0)); window.setTimeout(() => invalid.reportValidity(), 0); return }
    setBusy(true); setError('')
    try {
      const fields = new FormData(form.current)
      const configuration = { ...readConfiguration(fields), deployment_mode: sourceType === 'docker_image' ? 'image' : mode }
      validateManagedConfiguration(configuration)
      const app = createdID ? { id: createdID } : await request<ApplicationDetail>('/applications', { method: 'POST', body: JSON.stringify({ name: fields.get('name'), source_type: sourceType, source: source(fields), auto_start: fields.has('auto_start'), configuration }) })
      setCreatedID(app.id)
      if (createdID) await request(`/applications/${encodeURIComponent(app.id)}`, { method: 'PATCH', body: JSON.stringify({ name: fields.get('name'), configuration }) })
      const secrets = JSON.parse(String(fields.get('secret_environment') || '{}')) as Record<string, string>
      for (const [name, value] of Object.entries(secrets)) await request(`/applications/${encodeURIComponent(app.id)}/secrets/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify({ value }) })
      if (!databaseApplied) { setProgress('Przygotowywanie bazy danych'); await applyDatabaseChoice(app.id, database, setProgress); setDatabaseApplied(true) }
      if (deploy) await request(`/applications/${encodeURIComponent(app.id)}/deploy`, { method: 'POST' })
      navigate(`/apps/${encodeURIComponent(app.id)}`)
    } catch (cause) { setError(message(cause)) } finally { setBusy(false); setProgress('') }
  }
  if (user?.role === 'viewer') return <p role="alert">Tworzenie aplikacji wymaga roli Operator lub Administrator.</p>
  return <div className="acp"><header className="acp-header"><div><Link to="/apps">Aplikacje</Link><h1>Dodaj aplikację</h1><p>Wybierz źródło, runtime i bazę. DevBox uruchomi aplikację w Dockerze.</p></div></header>
    <nav className="acp-tabs" aria-label="Etapy kreatora">{stages.map((stage, index) => <button key={stage} type="button" className="secondary-button" disabled={busy} aria-current={step === index ? 'step' : undefined} onClick={() => { if (form.current) { try { captureSummary() } catch (cause) { setError(message(cause)); return } }; setStep(index) }}>{index + 1}. {stage}</button>)}</nav>
    {error && <p className="error-banner" role="alert">{error}{createdID && <> <Link to={`/apps/${encodeURIComponent(createdID)}`}>Otwórz zapisaną aplikację</Link></>}</p>}
    {(busy || progress) && <p role="status">{progress || 'Zapisywanie aplikacji…'}</p>}
    <form ref={form} noValidate onSubmit={(event) => void submit(event, (event.nativeEvent as SubmitEvent).submitter?.getAttribute('value') === 'deploy')}><fieldset className="acp-card" disabled={busy}>
      <section data-step="0" hidden={step !== 0}><h2>Źródło</h2><div className="acp-fields"><label>Nazwa<input name="name" required maxLength={120} autoComplete="off" /></label><label>Źródło<select aria-label="Źródło" disabled={!!createdID} value={sourceType} onChange={(event) => { setSourceType(event.target.value); setDetection(null); setMode(event.target.value === 'docker_image' ? 'image' : 'auto') }}>{Object.entries(sourceNames).map(([type, label]) => <option key={type} value={type}>{label}</option>)}</select></label>
        {sourceType === 'local' && <div className="application-directory-field"><label htmlFor="application-local-path">Katalog z kodem</label><div className="application-directory-row"><input id="application-local-path" required disabled={!!createdID} value={path} onChange={(event) => { setPath(event.target.value); setDetection(null) }} placeholder="/home/chris/apps/example lub /mnt/c/Projects/example" autoComplete="off" /><button type="button" className="secondary-button" onClick={() => setDirectoryOpen(true)}>Przeglądaj</button></div></div>}
        {sourceType === 'git' && <><label>Repozytorium Git<input name="repository_url" required placeholder="https://github.com/org/app.git" disabled={!!createdID} /></label><label>Gałąź / tag<input name="reference" placeholder="Domyślna gałąź" /></label><label>Zapisane dane Git<input name="credential_id" placeholder="Opcjonalny identyfikator poświadczeń" /></label><button type="button" disabled={detecting} onClick={() => void analyzeGit()}>Analizuj repozytorium</button></>}
        {sourceType === 'docker_image' && <label>Obraz OCI<input name="docker_image" required disabled={!!createdID} placeholder="nginx:1.28-alpine" /></label>}
        {sourceType === 'empty' && <p>DevBox utworzy katalog z działającą aplikacją startową dla wybranego runtime.</p>}
      </div>{directoryOpen && <DirectoryPicker value={path} onSelect={setPath} onClose={() => setDirectoryOpen(false)} />}{detecting && <p role="status">Analizowanie źródła…</p>}{detection && <DetectionResult value={detection} />}</section>
      <section hidden={step !== 1 && step !== 2}><h2>{step === 1 ? 'Technologia' : 'Uruchamianie'}</h2><ConfigurationFields key={`${sourceType}:${path}:${detection?.driver ?? ''}:${detection?.runtime ?? ''}`} value={{ runtime: ['dockerfile', 'image'].includes(detection?.runtime ?? '') ? '' : detection?.runtime ?? '', runtime_version: detection?.version ?? '', start_command: detection?.start_command ?? '', container_port: detection?.endpoints?.find((item) => item.primary)?.container_port ?? '', compose_service: detection?.services?.find((item) => item.primary)?.name ?? '' }} deploymentMode={mode} onDeploymentModeChange={setMode} wizardStep={step} />
        {step === 2 && mode === 'compose' && detection?.endpoints && <label>Główny endpoint HTTP<select defaultValue="" onChange={(event) => { const [service, port] = event.target.value.split(':'); const serviceInput = form.current?.elements.namedItem('compose_service') as HTMLInputElement | null; const portInput = form.current?.elements.namedItem('container_port') as HTMLInputElement | null; if (serviceInput) serviceInput.value = service; if (portInput) portInput.value = port }}><option value="">Wybierz usługę HTTP</option>{detection.endpoints.map((endpoint) => <option key={`${endpoint.service}:${endpoint.container_port}`} value={`${endpoint.service}:${endpoint.container_port}`}>{endpoint.service}:{endpoint.container_port}</option>)}</select></label>}
        <div data-step="2" hidden={step !== 2}><label className="acp-check"><input name="auto_start" type="checkbox" />Przywróć uruchomienie po restarcie DevBox</label></div></section>
      <section data-step="3" hidden={step !== 3}><h2>Baza danych</h2><DatabaseConfiguration value={database} onChange={(choice) => { setDatabase(choice); setDatabaseApplied(false) }} /></section>
      <section data-step="4" hidden={step !== 4}><h2>Podsumowanie</h2><dl className="acp-facts"><div><dt>Źródło</dt><dd>{sourceNames[sourceType]} {String(summary.source_description ?? path)}</dd></div><div><dt>Tryb</dt><dd>{mode}</dd></div><div><dt>Runtime</dt><dd>{String(summary.runtime ?? detection?.runtime ?? 'Własny obraz')} {String(summary.runtime_version ?? '')}</dd></div><div><dt>Obraz bazowy</dt><dd>{String(summary.runtime_image || 'Obrazy wskazane w Dockerfile / Compose')}</dd></div><div><dt>Kontenery</dt><dd>{detection?.services?.map((service) => service.name).join(', ') || 'web'}</dd></div><div><dt>Mount</dt><dd>{sourceType === 'docker_image' ? 'Bez katalogu źródłowego' : `${path || 'Katalog zarządzany DevBox'} → ${String(summary.mount_target || (mode === 'auto' ? detection?.profile === 'wordpress' ? '/var/www/html' : summary.runtime === 'static' ? '/usr/share/nginx/html' : '/app' : 'WORKDIR'))} (RW)`}</dd></div><div><dt>Porty</dt><dd>{String(summary.container_port ?? 'wykryty')} → {String(summary.host_port ?? 'automatycznie od 8080')}</dd></div><div><dt>Baza</dt><dd>{database.mode === 'none' ? 'Bez bazy' : database.mode === 'create' ? `${database.engine}: ${database.name}` : database.database_id}</dd></div><div><dt>Sieci</dt><dd>Sieć prywatna aplikacji + devbox-apps</dd></div></dl><p>Hasła trafią do SecretStore. Postęp wdrożenia pojawi się w zakładce Zadania.</p><div className="acp-actions"><button type="submit" value="save">Zapisz</button><button type="submit" value="deploy" disabled={detecting}>Utwórz i uruchom</button></div></section>
      <div className="acp-actions">{step > 0 && <button type="button" className="secondary-button" onClick={() => setStep(step - 1)}>Wstecz</button>}{step < stages.length - 1 && <button type="button" disabled={detecting} onClick={next}>Dalej</button>}</div>
    </fieldset></form>
  </div>
}
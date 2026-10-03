import { useCallback, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { request } from '../api/client'
import type { Job } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { usePolling } from '../control-room/usePolling'
import { ConfigurationFields, DetectionResult } from '../applications/components'
import { DirectoryPicker } from '../components/DirectoryPicker'
import { message, readConfiguration, sourceNames, type ApplicationDetail, type Configuration, type CreateApplication, type Detection } from '../applications/model'

export function ApplicationWizardPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const [step, setStep] = useState(0)
  const [source, setSource] = useState('git')
  const [localPath, setLocalPath] = useState('')
  const [directoryOpen, setDirectoryOpen] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [driver, setDriver] = useState('')
  const [runtime, setRuntime] = useState('')
  const [phpModules, setPHPModules] = useState<string[]>([])
  const [detected, setDetected] = useState<Detection | null>(null)
  const [analysisID, setAnalysisID] = useState('')
  const form = useRef<HTMLFormElement>(null)
  const loadAnalysis = useCallback((signal: AbortSignal) => request<Job>(`/jobs/${encodeURIComponent(analysisID)}`, { signal }), [analysisID])
  const analysis = usePolling(loadAnalysis, 2000, !!analysisID)
  const analysisJob = analysis.data?.id === analysisID ? analysis.data : null
  const analysisBusy = !!analysisID && (!analysisJob || ['queued', 'running'].includes(analysisJob.status)) && !analysis.error
  const analysisResult = (analysisID ? analysisJob?.result?.detection : undefined) as Detection | undefined
  const rawDetection = analysisResult ?? detected
  const detection = rawDetection ? { ...rawDetection, driver: driver || rawDetection.driver, ...(runtime ? { runtime } : {}) } : null
  const needsAnalysis = ['git', 'local'].includes(source) && !detection
  function payload(): CreateApplication {
    if (!form.current) throw new Error('Formularz niedostępny.')
    const fields = new FormData(form.current)
    const text = (key: string) => String(fields.get(key) ?? '').trim()
    const src: CreateApplication['source'] = {}
    if (source === 'git') { src.repository_url = text('repository_url'); src.reference = text('reference'); if (text('credential_id')) src.credential_id = text('credential_id') }
    if (source === 'local') src.local_path = text('local_path')
    if (source === 'docker_image') src.docker_image = text('docker_image')
    const useCompose = driver === 'compose'
    const configuration: Configuration = { ...readConfiguration(fields), ...(text('root_dir') && text('root_dir') !== '.' ? { root_dir: text('root_dir') } : {}), ...(!useCompose && runtime ? { runtime } : {}) }
    if (useCompose) { delete configuration.runtime; delete configuration.runtime_version; delete configuration.modules }
    else if (runtime === 'php' && phpModules.length) configuration.modules = phpModules
    return { name: text('name'), description: text('description'), source_type: source, source: src, driver, auto_start: fields.has('auto_start'), configuration }
  }
  function next() {
    setError('')
    const fields = form.current?.querySelectorAll<HTMLInputElement>('fieldset:not([hidden]) input')
    if (fields && Array.from(fields).some((input) => !input.reportValidity())) return
    try { payload(); if (step === 1) void detect(); setStep((value) => Math.min(value + 1, 2)) } catch (error) { setError(message(error)) }
  }
  async function detect() {
    setBusy(true); setError(''); setDetected(null); setAnalysisID('')
    try {
      const detectPayload = payload()
      if (driver) detectPayload.driver = driver
      const result = await request<{ detection?: Detection; job?: Job }>('/applications/detect', { method: 'POST', body: JSON.stringify(detectPayload) })
      setDetected(result.detection ?? null)
      setAnalysisID(result.job?.id ?? '')
    }
    catch (error) { setError(message(error)) } finally { setBusy(false) }
  }
  async function submit(event: FormEvent) {
    event.preventDefault(); if (step < 2) { next(); return }
    if (busy || analysisBusy) return
    if (needsAnalysis) { setError('Przeanalizuj źródło przed zapisaniem aplikacji.'); return }
    setError(''); setBusy(true)
    try { const app = await request<ApplicationDetail>('/applications', { method: 'POST', body: JSON.stringify(payload()) }); navigate(`/apps/${encodeURIComponent(app.id)}`) }
    catch (error) { setError(message(error)) } finally { setBusy(false) }
  }
  if (user?.role === 'viewer') return <p role="alert">Tworzenie aplikacji wymaga roli Operator lub Administrator.</p>
  return <div className="acp"><header className="acp-header"><div><Link to="/apps">Aplikacje</Link><h1>Dodaj aplikację</h1></div><span>Krok {step + 1} z 3</span></header>
    <nav className="acp-steps" aria-label="Kroki formularza">{['Źródło', 'Konfiguracja', 'Analiza i zapis'].map((label, index) => <span key={label} aria-current={index === step ? 'step' : undefined}>{index + 1}. {label}</span>)}</nav>
    {error && <p className="error-banner" role="alert">{error}</p>}
    <form ref={form} onSubmit={(event) => void submit(event)} noValidate>
      <fieldset className="acp-card" hidden={step !== 0} disabled={busy || analysisBusy} onChange={() => { setDetected(null); setAnalysisID('') }}><legend>Źródło aplikacji</legend><div className="acp-fields">
        <label>Nazwa<input name="name" required maxLength={120} autoComplete="off" /></label>
        <label>Rodzaj źródła<select value={source} onChange={(event) => { setSource(event.target.value); setDirectoryOpen(false); setDetected(null); setAnalysisID('') }}>{Object.entries(sourceNames).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
        <label className="acp-wide">Opis<input name="description" maxLength={500} /></label>
        {source === 'git' && <><label className="acp-wide">Adres repozytorium<input name="repository_url" required placeholder="https://github.com/owner/repository.git" /></label><label>Gałąź lub tag<input name="reference" placeholder="Domyślna gałąź repozytorium" /></label><label>ID zapisanych poświadczeń Git<input name="credential_id" placeholder="Opcjonalnie" /><small>Identyfikator z modułu Poświadczenia. Nie wpisuj tokenu.</small></label></>}
        {source === 'git' && <label className="acp-wide">Root dir aplikacji w repozytorium<input name="root_dir" defaultValue="." placeholder=". lub apps/portal" /><small>Katalog względem checkoutu, który zawiera kod tej aplikacji. Dla katalogu lokalnego podaj bezpośrednio ścieżkę root dir.</small></label>}
        {source === 'local' && <>
          <div className="acp-wide application-directory-field">
            <label htmlFor="application-local-path">Root dir aplikacji — katalog źródłowy</label>
            <div className="application-directory-row">
              <input
                id="application-local-path"
                name="local_path"
                required
                value={localPath}
                onChange={(event) => setLocalPath(event.target.value)}
                placeholder="/opt/devbox/projects/aplikacja"
                autoComplete="off"
              />
              <button type="button" className="secondary-button" onClick={() => setDirectoryOpen(true)}>Przeglądaj</button>
            </div>
            <small>Ten katalog jest root dir aplikacji i zostanie udostępniony kontenerowi. To ścieżka serwera / WSL, nie komputera przeglądarki.</small>
          </div>
          {directoryOpen && <DirectoryPicker
            value={localPath}
            onSelect={setLocalPath}
            onClose={() => setDirectoryOpen(false)}
          />}
        </>}
        {source === 'docker_image' && <label className="acp-wide">Obraz Docker / OCI<input name="docker_image" required placeholder="nginx:alpine" /></label>}
        {source === 'empty' && <p>Pusty katalog powstanie przy wdrożeniu. Przed deployem dodaj kod albo wybierz runtime static dla pustej strony.</p>}
      </div></fieldset>
      <fieldset className="acp-card" hidden={step !== 1} disabled={busy || analysisBusy} onChange={() => { setDetected(null); setAnalysisID('') }}><legend>Ustawienia wdrożenia</legend>
        <p className="muted">Aplikacja działa w kontenerze. Jeśli w katalogu źródłowym znajduje się Docker Compose, możesz zdecydować o jego użyciu po analizie.</p>
        <ConfigurationFields value={{ runtime, modules: phpModules }} driver={driver} onDriverChange={(value) => { setDriver(value); setDetected(null); setAnalysisID('') }} onRuntimeChange={(value) => { setRuntime(value); setPHPModules([]); setDetected(null); setAnalysisID('') }} onModulesChange={setPHPModules} /><label className="acp-check"><input type="checkbox" name="auto_start" />Przywróć uruchomienie po restarcie DevBox, gdy stan docelowy to „uruchomiona”.</label>
      </fieldset>
      <fieldset className="acp-card" hidden={step !== 2} disabled={busy}><legend>Analiza i zapis</legend>
        <p>Analiza nie uruchamia aplikacji. Zapis utworzy konfigurację; wdrożenie uruchomisz na ekranie szczegółów. Tam można najpierw dodać sekrety.</p>
        <button type="button" className="secondary-button" disabled={analysisBusy} onClick={() => void detect()}>{analysisBusy ? 'Analizowanie…' : 'Przeanalizuj źródło'}</button>
        {analysisID && <p><Link to={`/jobs?job=${encodeURIComponent(analysisID)}`}>Zadanie analizy</Link> · {analysisJob?.status ?? 'odczytywanie'}</p>}
        {analysis.error && <p role="alert">{analysis.error}</p>}
        {analysisJob?.error && <p role="alert">{analysisJob.error}</p>}
        {detection && <DetectionResult value={detection} />}
        {detection?.driver === 'compose' && <p className="acp-notice">W źródle wykryto Docker Compose. Możesz wybrać Docker Compose albo sterownik Managed w ustawieniach wdrożenia przed pierwszym wdrożeniem.</p>}
      </fieldset>
      <div className="acp-actions"><button type="button" className="secondary-button" disabled={step === 0 || busy || analysisBusy} onClick={() => setStep((value) => value - 1)}>Wstecz</button>{step < 2 ? <button type="button" onClick={next}>Dalej</button> : <button type="submit" disabled={busy || analysisBusy || needsAnalysis}>{busy ? 'Zapisywanie…' : 'Utwórz aplikację'}</button>}</div>
    </form>
  </div>
}

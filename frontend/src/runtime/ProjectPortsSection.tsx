import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { request } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import {
  defaultPortSettings,
  publishedApplicationURL,
  samePortSettings,
  validatePortSettings,
} from './portSettings'
import type { PortConfiguration, PortSettings } from './portSettings'

interface FieldsProps {
  settings: PortSettings
  disabled: boolean
  onChange: (change: Partial<PortSettings>) => void
}

export function PortSettingsFields({ settings, disabled, onChange }: FieldsProps) {
  const httpsAction = settings.https_enabled
    ? { label: 'Usuń HTTPS', change: { https_enabled: false } }
    : { label: '+ Dodaj HTTPS', change: { https_enabled: true } }
  const candidates = settings.candidates ?? []
  const infrastructure = settings.infrastructure_services ?? []
  const services = Array.from(new Set(candidates.map((candidate) => candidate.service)))
  if (settings.compose_service && !services.includes(settings.compose_service)) services.unshift(settings.compose_service)
  const proxyDisabled = settings.reverse_proxy_mode === 'disabled'

  function selectService(service: string) {
    const candidate = candidates.find((item) => item.service === service)
    onChange({
      compose_service: service,
      ...(candidate ? { container_port: candidate.port, protocol: candidate.protocol === 'https' ? 'https' : 'http' } : {}),
    })
  }

  return <fieldset disabled={disabled} aria-label="Mapowanie portów Docker" className="port-settings-fields">
    <div className="port-mapping-editor">
      <div className="port-mapping-toolbar">
        <div>
          <strong>Reverse proxy</strong>
          <small className="muted">DevBox wykrywa usługę HTTP aplikacji i pomija bazy danych, cache oraz inne usługi infrastrukturalne.</small>
        </div>
      </div>

      <div className="form-grid reverse-proxy-settings">
        <label>Tryb
          <select aria-label="Tryb reverse proxy" value={settings.reverse_proxy_mode}
            onChange={(event) => onChange({ reverse_proxy_mode: event.target.value as PortSettings['reverse_proxy_mode'] })}>
            <option value="automatic">Automatyczny</option>
            <option value="manual">Ręczny</option>
            <option value="disabled">Wyłączony</option>
          </select>
        </label>
        <label>Serwis
          <select aria-label="Serwis aplikacji" disabled={proxyDisabled} value={settings.compose_service}
            onChange={(event) => selectService(event.target.value)}>
            <option value="">Automatycznie</option>
            {services.map((service) => <option key={service} value={service}>{service}</option>)}
          </select>
        </label>
        <label>Port kontenera
          <input type="number" min={1} max={65535} step={1} disabled={proxyDisabled}
            aria-label="Port kontenera reverse proxy" value={settings.container_port || ''}
            placeholder="Auto"
            onChange={(event) => onChange({ container_port: Number(event.target.value) })} />
        </label>
        <label>Protokół
          <select aria-label="Protokół reverse proxy" disabled={proxyDisabled} value={settings.protocol}
            onChange={(event) => onChange({ protocol: event.target.value as PortSettings['protocol'] })}>
            <option value="http">HTTP</option>
            <option value="https">HTTPS</option>
          </select>
        </label>
        <label className="span-2">Healthcheck
          <input disabled={proxyDisabled} value={settings.healthcheck}
            placeholder="/ lub http://app:8000/health"
            onChange={(event) => onChange({ healthcheck: event.target.value })} />
        </label>
      </div>

      {settings.compose_service && settings.container_port > 0 && <div className="validation-box reverse-proxy-detection">
        <strong>Wykryto: {settings.compose_service}:{settings.container_port}</strong>
        <span>Źródło: {settings.detection_source || (settings.detection_mode === 'manual' ? 'konfiguracja ręczna' : 'docker-compose.yml')}</span>
      </div>}
      {candidates.length > 1 && <div className="validation-box">
        <strong>Wymagany wybór portu aplikacji.</strong>
        <span>{candidates.map((candidate) => `${candidate.service}:${candidate.port}`).join(', ')}</span>
      </div>}
      {infrastructure.length > 0 && <div className="port-mapping-help">
        <span><strong>Pomijane usługi infrastrukturalne:</strong> {infrastructure.map((candidate) => `${candidate.service}:${candidate.port}`).join(', ')}</span>
      </div>}

      <div className="port-mapping-toolbar">
        <div>
          <strong>Mapowania portów</strong>
          <small className="muted">Port hosta jest publikowany na zewnątrz, a port kontenera wskazuje port aplikacji wewnątrz Dockera.</small>
        </div>
        <button type="button" className="secondary-button port-mapping-toggle" disabled={proxyDisabled}
          onClick={() => onChange(httpsAction.change)}>
          {httpsAction.label}
        </button>
      </div>

      <div className="port-mapping-table-wrap">
        <table className="port-mapping-table">
          <thead>
            <tr>
              <th>Port hosta</th>
              <th>Port kontenera</th>
              <th>Typ</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>
                <div className="port-mapping-cell">
                  <span className="port-map-kind">HTTP</span>
                  <input type="number" required min={1} max={65535} step={1} disabled={proxyDisabled}
                    aria-label="Port hosta HTTP" value={settings.host_port || ''}
                    onChange={(event) => onChange({ host_port: Number(event.target.value) })} />
                </div>
              </td>
              <td>
                <input type="number" min={1} max={65535} step={1} disabled={proxyDisabled}
                  aria-label="Port kontenera HTTP" value={settings.container_port || ''}
                  placeholder="Auto"
                  onChange={(event) => onChange({ container_port: Number(event.target.value) })} />
              </td>
              <td>
                <select aria-label="Typ protokołu HTTP" value="tcp" disabled={proxyDisabled} onChange={() => undefined}>
                  <option value="tcp">TCP</option>
                </select>
              </td>
            </tr>
            {settings.https_enabled && <tr>
              <td>
                <div className="port-mapping-cell">
                  <span className="port-map-kind">HTTPS</span>
                  <input type="number" required min={1} max={65535} step={1} disabled={proxyDisabled}
                    aria-label="Port hosta HTTPS" value={settings.https_host_port || ''}
                    onChange={(event) => onChange({ https_host_port: Number(event.target.value) })} />
                </div>
              </td>
              <td>
                <input type="number" required min={1} max={65535} step={1} disabled={proxyDisabled}
                  aria-label="Port kontenera HTTPS" value={settings.https_container_port || ''}
                  onChange={(event) => onChange({ https_container_port: Number(event.target.value) })} />
              </td>
              <td>
                <select aria-label="Typ protokołu HTTPS" value="tcp" disabled={proxyDisabled} onChange={() => undefined}>
                  <option value="tcp">TCP</option>
                </select>
              </td>
            </tr>}
          </tbody>
        </table>
      </div>

      <div className="port-mapping-help">
        <span><strong>HTTP:</strong> puste pole portu kontenera oznacza Auto — DevBox wykryje Compose, EXPOSE, healthcheck lub runtime.</span>
        <span><strong>Host:</strong> od wskazanego portu DevBox szuka kolejnego wolnego numeru, np. 8080, 8081, 8082.</span>
        {settings.https_enabled && <span><strong>HTTPS:</strong> to passthrough TCP. DevBox nie tworzy certyfikatu ani serwera TLS.</span>}
      </div>
    </div>
  </fieldset>
}
export function ProjectPortsSection({ projectId }: { projectId: string }) {
  const { user } = useAuth()
  const [settings, setSettings] = useState<PortSettings>({ ...defaultPortSettings })
  const [config, setConfig] = useState<PortConfiguration | null>(null)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [lastJob, setLastJob] = useState<string | null>(null)
  const [pollingJob, setPollingJob] = useState<string | null>(null)
  const canEdit = user?.role === 'admin' || user?.role === 'operator'
  const path = `/projects/${encodeURIComponent(projectId)}/ports/config`

  useEffect(() => {
    const controller = new AbortController()
    setConfig(null)
    setError('')
    setMessage('')
    setLastJob(null)
    setPollingJob(null)
    request<PortConfiguration>(path, { signal: controller.signal })
      .then((value) => {
        if (controller.signal.aborted) return
        setConfig(value)
        setSettings(value.settings)
      })
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : 'Nie udało się wczytać konfiguracji portów.')
      })
    return () => controller.abort()
  }, [path])

  useEffect(() => {
    if (!pollingJob) return
    const jobToPoll = pollingJob
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    async function poll() {
      try {
        const [value, job] = await Promise.all([
          request<PortConfiguration>(path, { signal: controller.signal }),
          request<{ status: string }>(`/jobs/${encodeURIComponent(jobToPoll)}`, { signal: controller.signal }),
        ])
        if (controller.signal.aborted) return
        setConfig(value)
        if (['succeeded', 'success', 'completed', 'failed', 'cancelled', 'canceled'].includes(job.status.toLowerCase())) {
          setPollingJob(null)
          setSettings(value.settings)
          setMessage(`Zadanie wdrożenia zakończone: ${job.status}. Poniżej widoczna jest ostatnia zastosowana konfiguracja portów.`)
          return
        }
      } catch (reason: unknown) {
        if (controller.signal.aborted) return
        setError(reason instanceof Error ? reason.message : 'Nie udało się odświeżyć stanu portów.')
      }
      if (!controller.signal.aborted) timer = setTimeout(() => void poll(), 3000)
    }
    void poll()
    return () => { controller.abort(); if (timer) clearTimeout(timer) }
  }, [path, pollingJob])

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const deploy = (event.nativeEvent as SubmitEvent).submitter?.getAttribute('value') === 'deploy'
    const normalized = { ...settings, compose_service: settings.compose_service.trim() }
    const validation = validatePortSettings(normalized)
    if (validation) { setError(validation); return }
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const saved = await request<PortConfiguration>(path, { method: 'PUT', body: JSON.stringify(normalized) })
      setConfig(saved)
      setSettings(saved.settings)
      setMessage('Porty zapisane. Zmiana zacznie działać przy następnym wdrożeniu. Obecny kontener nie został zatrzymany.')
      if (deploy) {
        const job = await request<{ id: string }>(`/projects/${encodeURIComponent(projectId)}/deploy`, { method: 'POST', body: '{}' })
        setLastJob(job.id)
        setPollingJob(job.id)
        setMessage('Konfiguracja zapisana. Wdrożenie jest w kolejce; przydzielone porty pojawią się poniżej po uruchomieniu kontenera.')
      }
    } catch (reason: unknown) {
      setError(reason instanceof Error ? reason.message : 'Nie udało się zapisać lub wdrożyć mapowania portów.')
    } finally {
      setBusy(false)
    }
  }

  const pending = config?.applied && !samePortSettings(config.settings, config.applied.settings)
  const baseURL = typeof window === 'undefined' ? 'http://localhost/' : window.location.href

  return <section className="runtime-section panel" aria-label="Konfiguracja portów Docker">
    <div className="section-heading"><div>
      <h2>Porty Docker i dostęp do aplikacji</h2>
      <p className="muted">Zarządzaj mapowaniami host → kontener w jednej tabeli. Rezerwacje innych projektów oraz zajęte gniazda są sprawdzane przy wdrożeniu.</p>
    </div></div>
    {error && <div className="error-banner" role="alert">{error}</div>}
    {message && <div className="validation-box" role="status">{message}</div>}
    {lastJob && <p><a href={`/jobs?job=${encodeURIComponent(lastJob)}`}>Otwórz status i logi wdrożenia</a></p>}
    {!config && !error && <p className="muted">Wczytywanie konfiguracji portów…</p>}
    {config && <form onSubmit={(event) => void save(event)}>
      <PortSettingsFields settings={settings} disabled={!canEdit || busy || pollingJob !== null}
        onChange={(change) => setSettings((current) => ({ ...current, ...change }))} />
      {canEdit && <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.75rem', marginTop: '1rem' }}>
        <button type="submit" value="save" disabled={busy || pollingJob !== null}>Zapisz porty</button>
        <button type="submit" value="deploy" disabled={busy || pollingJob !== null}>{busy ? 'Zapisywanie…' : 'Zapisz i wdroż'}</button>
      </div>}
    </form>}
    {config?.applied ? <>
      <h3>Ostatnio zastosowane mapowanie</h3>
      {pending && <p className="muted">Zapisane ustawienia różnią się od działającego mapowania. Wdróż projekt, aby je zastosować.</p>}
      <div className="port-mapping-table-wrap port-mapping-applied">
        <table className="port-mapping-table">
          <thead><tr><th>Port hosta</th><th>Port kontenera</th><th>Typ</th></tr></thead>
          <tbody>
            <tr>
              <td><div className="port-mapping-cell"><span className="port-map-kind">HTTP</span><a target="_blank" rel="noopener noreferrer"
                href={publishedApplicationURL(baseURL, config.applied.http.host_port, false)}>{config.applied.http.host_port}</a></div></td>
              <td>{config.applied.http.container_port}</td>
              <td>TCP</td>
            </tr>
            {config.applied.https && <tr>
              <td><div className="port-mapping-cell"><span className="port-map-kind">HTTPS</span><a target="_blank" rel="noopener noreferrer"
                href={publishedApplicationURL(baseURL, config.applied.https.host_port, true)}>{config.applied.https.host_port}</a></div></td>
              <td>{config.applied.https.container_port}</td>
              <td>TCP</td>
            </tr>}
          </tbody>
        </table>
      </div>
    </> : <p className="muted">Przydzielone mapowanie pojawi się po udanym wdrożeniu.</p>}
    <p className="muted">Porty są publikowane zgodnie z domyślnym nasłuchiwaniem Dockera, zwykle na wszystkich interfejsach hosta. Dostęp z Internetu wymaga odpowiednich reguł firewalla, a przy NAT/WSL także przekierowania ruchu. DevBox nie otwiera automatycznie portów na routerze.</p>
  </section>
}

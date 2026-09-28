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
  return <fieldset disabled={disabled} aria-label="Mapowanie portów Docker" style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
    <div className="form-grid">
      <label>Port wewnętrzny HTTP (Docker)
        <input type="number" min={1} max={65535} step={1} value={settings.container_port || ''}
          placeholder="Automatycznie — EXPOSE lub runtime"
          onChange={(event) => onChange({ container_port: Number(event.target.value) })} />
        <small className="muted">Puste pole: wykryj port. Przy własnym Dockerfile aplikacja musi na nim już nasłuchiwać.</small>
      </label>
      <label>Port zewnętrzny HTTP (host)
        <input type="number" required min={1} max={65535} step={1} value={settings.host_port || ''}
          onChange={(event) => onChange({ host_port: Number(event.target.value) })} />
        <small className="muted">Domyślnie 8080. Gdy zajęty, DevBox wybierze 8081, 8082 itd. Przydzielony port zostanie zachowany.</small>
      </label>
      <label className="span-2" style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        <input type="checkbox" checked={settings.https_enabled}
          onChange={(event) => onChange({ https_enabled: event.target.checked })} />
        Włącz dodatkowe mapowanie HTTPS
      </label>
      <label>Port wewnętrzny HTTPS (Docker)
        <input type="number" required min={1} max={65535} step={1} disabled={!settings.https_enabled}
          value={settings.https_container_port || ''}
          onChange={(event) => onChange({ https_container_port: Number(event.target.value) })} />
      </label>
      <label>Port zewnętrzny HTTPS (host)
        <input type="number" required min={1} max={65535} step={1} disabled={!settings.https_enabled}
          value={settings.https_host_port || ''}
          onChange={(event) => onChange({ https_host_port: Number(event.target.value) })} />
        <small className="muted">Domyślnie 8443; przy kolizji kolejny wolny port, zwiększany o 1.</small>
      </label>
      <p className="muted span-2">HTTPS jest przekazywane do kontenera, nie tworzy certyfikatu ani serwera TLS. Wymagany własny Dockerfile lub Compose z działającym HTTPS. Obrazy generowane przez DevBox udostępniają HTTP.</p>
      <label className="span-2">Usługa Compose (opcjonalnie)
        <input value={settings.compose_service} maxLength={128} placeholder="np. web — puste: wykryj jednoznacznie usługę web"
          onChange={(event) => onChange({ compose_service: event.target.value })} />
        <small className="muted">Dla Compose zapis włącza zarządzanie publikowanymi portami wybranej usługi. Wymaga Docker Compose 2.24.4+. Inne usługi oraz pliki źródłowe projektu pozostają bez zmian.</small>
      </label>
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
      <p className="muted">Osobno wybierz port aplikacji w kontenerze i port dostępny na hoście. Rezerwacje innych projektów oraz zajęte gniazda są sprawdzane przy wdrożeniu.</p>
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
      <dl className="runtime-summary">
        <div><dt>HTTP: host → kontener</dt><dd><a target="_blank" rel="noopener noreferrer"
          href={publishedApplicationURL(baseURL, config.applied.http.host_port, false)}>{config.applied.http.host_port} → {config.applied.http.container_port}</a></dd></div>
        {config.applied.https && <div><dt>HTTPS: host → kontener</dt><dd><a target="_blank" rel="noopener noreferrer"
          href={publishedApplicationURL(baseURL, config.applied.https.host_port, true)}>{config.applied.https.host_port} → {config.applied.https.container_port}</a></dd></div>}
      </dl>
    </> : <p className="muted">Przydzielone mapowanie pojawi się po udanym wdrożeniu.</p>}
    <p className="muted">Porty są publikowane zgodnie z domyślnym nasłuchiwaniem Dockera, zwykle na wszystkich interfejsach hosta. Dostęp z Internetu wymaga odpowiednich reguł firewalla, a przy NAT/WSL także przekierowania ruchu. DevBox nie otwiera automatycznie portów na routerze.</p>
  </section>
}

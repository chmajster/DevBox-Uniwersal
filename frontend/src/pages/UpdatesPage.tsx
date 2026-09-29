import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { request } from '../api/client'
import type {
  DockerStatus,
  MySQLStatus,
  ProxyStatus,
  SystemComponentStatus,
  SystemInfo,
  SystemPlatformInfo,
  UpdateProgress,
  UpdateStatus
} from '../api/types'
import { Icon } from '../components/Icon'
import {
  UPDATE_STAGES,
  buildUpdateHardRefreshURL,
  clampUpdatePercent,
  clearUpdateHardRefreshURL,
  updateIsActive,
  updateStageState,
  updateStateLabel
} from '../updates/progress'

type UpdateTab = 'update' | 'environment' | 'advanced'

const componentLabels: Record<string, string> = {
  git: 'Git',
  docker: 'Docker',
  nginx: 'Nginx',
  mysql: 'MySQL / MariaDB',
  php: 'PHP',
  composer: 'Composer',
  python: 'Python',
  pip: 'pip',
  go: 'Go',
  node: 'Node.js',
  npm: 'npm'
}

function shortVersion(value?: string) {
  if (!value) return '—'
  return /^[a-f0-9]{40}$/i.test(value) ? value.slice(0, 10) : value
}

function formatDate(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('pl-PL', { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}

function platformLabel(platform: SystemPlatformInfo | null, info: SystemInfo | null) {
  if (!platform) return info ? `${info.os} / ${info.arch}` : '—'
  const distro = platform.distro_name || platform.distro_id || platform.os
  if (platform.wsl) return `${distro} · WSL${platform.wsl_version || ''}`
  return `${distro} · native`
}

function statusAttribute(value?: boolean) {
  if (value === undefined) return undefined
  return value ? 'true' : 'false'
}

function tabFromSearch(value: string | null): UpdateTab {
  if (value === 'environment' || value === 'advanced') return value
  return 'update'
}

export function UpdatesPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab = tabFromSearch(searchParams.get('tab'))

  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [progress, setProgress] = useState<UpdateProgress | null>(null)
  const [systemInfo, setSystemInfo] = useState<SystemInfo | null>(null)
  const [platform, setPlatform] = useState<SystemPlatformInfo | null>(null)
  const [components, setComponents] = useState<SystemComponentStatus[]>([])
  const [docker, setDocker] = useState<DockerStatus | null>(null)
  const [mysql, setMySQL] = useState<MySQLStatus | null>(null)
  const [proxy, setProxy] = useState<ProxyStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [checking, setChecking] = useState(false)
  const [environmentLoading, setEnvironmentLoading] = useState(false)
  const [environmentLoaded, setEnvironmentLoaded] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [environmentWarning, setEnvironmentWarning] = useState('')
  const updateWasActive = useRef(false)
  const hardRefreshStarted = useRef(false)

  const loadStatus = useCallback(async () => {
    setChecking(true)
    try {
      const data = await request<UpdateStatus>('/update/status')
      setStatus(data)
      return data
    } finally {
      setChecking(false)
    }
  }, [])

  const loadEnvironment = useCallback(async () => {
    setEnvironmentLoading(true)
    setEnvironmentWarning('')
    try {
      const [infoResult, platformResult, componentsResult, dockerResult, mysqlResult, proxyResult] = await Promise.allSettled([
        request<SystemInfo>('/system/info'),
        request<SystemPlatformInfo>('/system/platform'),
        request<SystemComponentStatus[]>('/system/components'),
        request<DockerStatus>('/docker/status'),
        request<MySQLStatus>('/mysql/status'),
        request<ProxyStatus>('/proxy/status')
      ])

      if (infoResult.status === 'fulfilled') setSystemInfo(infoResult.value)
      if (platformResult.status === 'fulfilled') setPlatform(platformResult.value)
      if (componentsResult.status === 'fulfilled') setComponents(componentsResult.value)
      if (dockerResult.status === 'fulfilled') setDocker(dockerResult.value)
      if (mysqlResult.status === 'fulfilled') setMySQL(mysqlResult.value)
      if (proxyResult.status === 'fulfilled') setProxy(proxyResult.value)

      const missing: string[] = []
      if (infoResult.status === 'rejected') missing.push('host')
      if (platformResult.status === 'rejected') missing.push('platforma')
      if (componentsResult.status === 'rejected') missing.push('komponenty')
      if (dockerResult.status === 'rejected') missing.push('Docker')
      if (mysqlResult.status === 'rejected') missing.push('MySQL')
      if (proxyResult.status === 'rejected') missing.push('Nginx')
      if (missing.length) setEnvironmentWarning(`Nie udało się odczytać części informacji środowiska: ${missing.join(', ')}.`)
      setEnvironmentLoaded(true)
    } finally {
      setEnvironmentLoading(false)
    }
  }, [])

  const loadProgress = useCallback(async () => {
    const data = await request<UpdateProgress>('/update/progress')
    setProgress(data)
    return data
  }, [])

  useEffect(() => {
    const cleanURL = clearUpdateHardRefreshURL(window.location.href)
    if (cleanURL !== window.location.href) window.history.replaceState(window.history.state, '', cleanURL)
    loadStatus().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się sprawdzić aktualizacji'))
    loadProgress().catch(() => undefined)
  }, [loadProgress, loadStatus])

  useEffect(() => {
    if (activeTab !== 'environment' || environmentLoaded || environmentLoading) return
    loadEnvironment().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać informacji środowiska'))
  }, [activeTab, environmentLoaded, environmentLoading, loadEnvironment])

  useEffect(() => {
    const interval = updateIsActive(progress) ? 1500 : 30000
    const timer = window.setInterval(() => {
      loadProgress().catch(() => undefined)
    }, interval)
    return () => window.clearInterval(timer)
  }, [loadProgress, progress?.state])

  useEffect(() => {
    if (progress?.state === 'starting' || progress?.state === 'running') {
      updateWasActive.current = true
      return
    }
    if (progress?.state === 'succeeded' && updateWasActive.current && !hardRefreshStarted.current) {
      hardRefreshStarted.current = true
      updateWasActive.current = false
      const refreshURL = buildUpdateHardRefreshURL(window.location.href, Date.now())
      window.location.replace(refreshURL)
      return
    }
    if (progress?.state === 'succeeded' || progress?.state === 'no_update') {
      loadStatus().catch(() => undefined)
    }
  }, [progress?.state, loadStatus])

  async function checkUpdates() {
    setError('')
    setMessage('')
    try {
      await loadStatus()
      await loadProgress().catch(() => undefined)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się sprawdzić aktualizacji')
    }
  }

  async function refreshEnvironment() {
    setError('')
    try {
      await loadEnvironment()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się odświeżyć informacji środowiska')
    }
  }

  async function apply() {
    if (!window.confirm('Uruchomić aktualizację DevBox z repozytorium Git? Backend i frontend zostaną przebudowane, a devbox.service po wdrożeniu zostanie zrestartowany.')) return
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const result = await request<{ status: string; message: string }>('/update/apply', { method: 'POST' })
      setMessage(result.message)
      setProgress({
        state: 'starting',
        percent: 1,
        stage: 'starting',
        message: 'Uruchamianie devbox-update.service…',
        current_version: status?.current_version,
        target_version: status?.latest_version,
        updated_at: new Date().toISOString()
      })
      window.setTimeout(() => loadProgress().catch(() => undefined), 500)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się uruchomić aktualizacji')
    } finally {
      setBusy(false)
    }
  }

  function changeTab(tab: UpdateTab) {
    if (tab === 'update') {
      setSearchParams({})
      return
    }
    setSearchParams({ tab })
  }

  const proxyOK = proxy ? proxy.detected && proxy.config_valid : undefined
  const mysqlOK = mysql ? mysql.running : undefined
  const dockerOK = docker ? docker.available : undefined
  const apiOK = systemInfo ? true : undefined
  const progressPercent = clampUpdatePercent(progress?.percent)
  const progressActive = updateIsActive(progress)
  const progressFailed = progress?.state === 'failed'
  const progressFinished = progress?.state === 'succeeded' || progress?.state === 'no_update'
  const showProgress = Boolean(progress && progress.state !== 'idle' && progress.state !== 'unknown')
  const currentStageLabel = progress?.stage && progress.stage !== 'idle'
    ? UPDATE_STAGES.find((stage) => stage.id === progress.stage)?.label || progress.stage
    : 'Oczekiwanie'

  return <>
    <div className="page-heading update-page-heading">
      <div>
        <h1>Aktualizacje</h1>
        <p className="muted">Zarządzanie wersją DevBox Universal, postęp wdrożenia i diagnostyka środowiska.</p>
      </div>
      {activeTab === 'update' && <button type="button" className="secondary" onClick={() => void checkUpdates()} disabled={busy || checking}>
        <Icon name="refresh" size={16} /> {checking ? 'Sprawdzanie…' : 'Sprawdź aktualizacje'}
      </button>}
      {activeTab === 'environment' && <button type="button" className="secondary" onClick={() => void refreshEnvironment()} disabled={environmentLoading}>
        <Icon name="refresh" size={16} /> {environmentLoading ? 'Odświeżanie…' : 'Odśwież środowisko'}
      </button>}
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}

    <nav className="tabs update-tabs" aria-label="Sekcje aktualizacji">
      <button type="button" className={activeTab === 'update' ? 'active' : ''} onClick={() => changeTab('update')}>Aktualizacja</button>
      <button type="button" className={activeTab === 'environment' ? 'active' : ''} onClick={() => changeTab('environment')}>Środowisko</button>
      <button type="button" className={activeTab === 'advanced' ? 'active' : ''} onClick={() => changeTab('advanced')}>Zaawansowane</button>
    </nav>

    {activeTab === 'update' && <>
      <section className="panel update-release-card">
        <div className="update-release-header">
          <div>
            <span className="eyebrow">DEVBOX UNIVERSAL</span>
            <h2>{status?.update_available ? 'Dostępna jest nowa wersja' : 'System jest aktualny'}</h2>
            <p className="muted small">Porównanie uruchomionej wersji z gałęzią <code>{status?.ref || 'main'}</code>.</p>
          </div>
          <span className="status-chip" data-ok={statusAttribute(status ? !status.update_available : undefined)}>
            {status ? status.update_available ? 'Dostępna aktualizacja' : 'System aktualny' : 'Sprawdzanie'}
          </span>
        </div>

        <div className="update-version-compare">
          <article>
            <span>Zainstalowana wersja</span>
            <strong className="mono">{shortVersion(status?.current_version)}</strong>
            <small>Aktualnie uruchomiony build</small>
          </article>
          <div className="update-version-arrow">→</div>
          <article>
            <span>Dostępna wersja</span>
            <strong className="mono">{shortVersion(status?.latest_version)}</strong>
            <small>{formatDate(status?.latest_commit_at)}</small>
          </article>
        </div>

        <div className="update-release-meta">
          <div><span>Repozytorium</span><strong>{status?.repository ?? '—'}</strong></div>
          <div><span>Gałąź</span><strong>{status?.ref ?? '—'}</strong></div>
          <div><span>Ostatnie sprawdzenie</span><strong>{formatDate(status?.checked_at)}</strong></div>
        </div>

        {status?.last_error && <div className="warning-banner">Nie udało się pobrać najnowszego commita: {status.last_error}</div>}

        <div className="update-primary-actions">
          <button type="button" className="secondary" onClick={() => void checkUpdates()} disabled={busy || checking}>
            <Icon name="refresh" size={16} /> {checking ? 'Sprawdzanie…' : 'Sprawdź'}
          </button>
          <button type="button" onClick={apply} disabled={busy || progressActive || !status?.update_available}>
            {busy ? 'Uruchamianie…' : progressActive ? 'Aktualizacja trwa…' : 'Aktualizuj teraz'}
          </button>
        </div>
      </section>

      <div className="update-dashboard-grid">
        <section className="panel update-mini-panel">
          <div className="update-mini-heading">
            <div>
              <span className="eyebrow">AUTO-UPDATE</span>
              <h2>Automatyczne aktualizacje</h2>
            </div>
            <span className="status-chip" data-ok={statusAttribute(status?.auto_update)}>
              {status?.auto_update ? 'Włączone' : 'Wyłączone'}
            </span>
          </div>
          <div className="update-mini-details">
            <div><span>Harmonogram</span><strong>{status?.schedule ?? '—'}</strong></div>
            <div><span>Źródło</span><strong><code>{status?.ref ?? 'main'}</code></strong></div>
          </div>
        </section>

        <section className="panel update-mini-panel">
          <div className="update-mini-heading">
            <div>
              <span className="eyebrow">AKTUALNY STAN</span>
              <h2>Updater</h2>
            </div>
            <span className="status-chip" data-ok={progressFinished ? 'true' : progressFailed ? 'false' : undefined}>{updateStateLabel(progress)}</span>
          </div>
          <div className="update-mini-details">
            <div><span>Etap</span><strong>{currentStageLabel}</strong></div>
            <div><span>Ostatnia zmiana</span><strong>{formatDate(progress?.updated_at)}</strong></div>
          </div>
        </section>
      </div>

      {showProgress && <section className={`panel update-progress-panel ${progressFailed ? 'is-failed' : progressFinished ? 'is-complete' : progressActive ? 'is-running' : ''}`} aria-live="polite">
        <div className="update-progress-heading">
          <div>
            <span className="eyebrow">POSTĘP AKTUALIZACJI</span>
            <h2>{progress?.message || currentStageLabel}</h2>
            <p className="muted small">
              {progressFailed
                ? 'Aktualizacja została zatrzymana na etapie pokazanym poniżej.'
                : progressFinished
                  ? 'Proces aktualizacji został zakończony.'
                  : 'Aktualny stan jest odczytywany z devbox-update.service.'}
            </p>
          </div>
          <div className="update-progress-value">
            <strong>{progressPercent}%</strong>
            <span className="status-chip" data-ok={progressFinished ? 'true' : progressFailed ? 'false' : undefined}>{updateStateLabel(progress)}</span>
          </div>
        </div>

        <div className="update-progress-bar">
          <progress max={100} value={progressPercent}>{progressPercent}%</progress>
          <div><span>0%</span><span>{currentStageLabel}</span><span>100%</span></div>
        </div>

        <div className="update-stage-track" aria-label="Etapy aktualizacji">
          {UPDATE_STAGES.map((stage, index) => {
            const stageState = updateStageState(progress, stage.id)
            return <div key={stage.id} className={`update-stage-dot is-${stageState}`}>
              <span>{stageState === 'done' ? <Icon name="check" size={12} /> : index + 1}</span>
              <small>{stage.label}</small>
            </div>
          })}
        </div>

        <div className="update-progress-summary">
          <div><span>Z wersji</span><strong className="mono">{shortVersion(progress?.current_version || status?.current_version)}</strong></div>
          <div><span>Do wersji</span><strong className="mono">{shortVersion(progress?.target_version || status?.latest_version)}</strong></div>
          <div><span>Rozpoczęto</span><strong>{formatDate(progress?.started_at)}</strong></div>
          <div><span>Ostatnia zmiana</span><strong>{formatDate(progress?.updated_at)}</strong></div>
        </div>

        {progressFailed && <div className="update-failure-box">
          <div className="update-failure-heading">
            <strong>Aktualizacja nie powiodła się</strong>
            <span>{currentStageLabel} · {progressPercent}%</span>
          </div>

          <div className="update-failure-meta">
            <div><span>Etap techniczny</span><code>{progress?.stage || '—'}</code></div>
            <div><span>Kod wyjścia</span><strong>{progress?.exit_code ?? '—'}</strong></div>
            <div><span>Log updatera</span><code>{progress?.log_path || '/var/log/devbox-update.log'}</code></div>
            <div><span>Zakończono</span><strong>{formatDate(progress?.finished_at || progress?.updated_at)}</strong></div>
          </div>

          <div className="update-failure-error">
            <span>Dokładny błąd</span>
            <pre>{progress?.failure_detail || progress?.error || 'Updater zakończył się błędem bez dodatkowego komunikatu.'}</pre>
            {progress?.failure_detail && progress?.error && progress.failure_detail !== progress.error &&
              <small className="muted">Updater: {progress.error}</small>}
          </div>

          {progress?.log_tail && progress.log_tail.length > 0 && <details className="update-failure-log" open>
            <summary>Ostatnie wpisy z logu ({progress.log_tail.length})</summary>
            <pre>{progress.log_tail.join('\n')}</pre>
          </details>}

          <div className="update-failure-actions">
            <button type="button" onClick={apply} disabled={busy || progressActive}>Spróbuj ponownie</button>
          </div>
        </div>}
      </section>}
    </>}

    {activeTab === 'environment' && <>
      {environmentWarning && <div className="warning-banner">{environmentWarning}</div>}

      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Stan usług</h2>
            <p className="muted small">Najważniejsze zależności działającej instancji DevBox.</p>
          </div>
        </div>
        <div className="update-service-grid">
          <div className="update-service-row">
            <div><strong>DevBox API</strong><small>{systemInfo?.go_version || 'brak danych'}</small></div>
            <span className="status-chip" data-ok={statusAttribute(apiOK)}>{apiOK ? 'Działa' : 'Brak danych'}</span>
          </div>
          <div className="update-service-row">
            <div><strong>Docker Engine</strong><small>{docker?.server_version || docker?.client_version || docker?.error || 'brak danych'}</small></div>
            <span className="status-chip" data-ok={statusAttribute(dockerOK)}>{dockerOK === undefined ? 'Brak danych' : dockerOK ? 'Działa' : 'Niedostępny'}</span>
          </div>
          <div className="update-service-row">
            <div><strong>Nginx</strong><small>{proxy?.version || proxy?.error || 'brak danych'}</small></div>
            <span className="status-chip" data-ok={statusAttribute(proxyOK)}>{proxyOK === undefined ? 'Brak danych' : proxyOK ? 'Konfiguracja OK' : 'Problem'}</span>
          </div>
          <div className="update-service-row">
            <div><strong>MySQL / MariaDB</strong><small>{mysql?.version || mysql?.connection_state || 'brak danych'}</small></div>
            <span className="status-chip" data-ok={statusAttribute(mysqlOK)}>{mysqlOK === undefined ? 'Brak danych' : mysqlOK ? 'Działa' : 'Niedostępny'}</span>
          </div>
        </div>
      </section>

      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Środowisko tej instancji</h2>
            <p className="muted small">Dane odczytywane bezpośrednio z hosta.</p>
          </div>
        </div>
        <div className="update-environment-grid">
          <article className="update-info-card">
            <span className="update-card-label">Host</span>
            <strong>{systemInfo?.hostname || '—'}</strong>
            <small>{platform?.distro_name || systemInfo?.os || 'System nieznany'}</small>
            <code>{platform?.arch || systemInfo?.arch || '—'}</code>
          </article>
          <article className="update-info-card">
            <span className="update-card-label">Platforma</span>
            <strong>{platformLabel(platform, systemInfo)}</strong>
            <small>{platform?.systemd ? 'systemd aktywny' : 'systemd niedostępny lub nieaktywny'}</small>
            <code>{platform?.wsl ? `WSL${platform.wsl_version || ''}` : 'Linux host'}</code>
          </article>
          <article className="update-info-card">
            <span className="update-card-label">Backend / API</span>
            <strong>Go · devbox.service</strong>
            <small>{systemInfo?.go_version || 'Wersja Go niedostępna'}</small>
            <code>{shortVersion(systemInfo?.version || status?.current_version)}</code>
          </article>
          <article className="update-info-card">
            <span className="update-card-label">Frontend</span>
            <strong>React + TypeScript SPA</strong>
            <small>Build statyczny serwowany przez proces DevBox</small>
            <code>/frontend/dist</code>
          </article>
          <article className="update-info-card">
            <span className="update-card-label">Stan aplikacji</span>
            <strong>SQLite</strong>
            <small>Konfiguracja, projekty, joby i metadane control plane</small>
            <code>devbox.db</code>
          </article>
          <article className="update-info-card">
            <span className="update-card-label">Provisioning baz</span>
            <strong>MySQL / MariaDB</strong>
            <small>{mysql?.version || mysql?.connection_state || 'Stan niedostępny'}</small>
            <code>{mysql?.running ? 'running' : 'not running'}</code>
          </article>
        </div>
      </section>

      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Komponenty hosta</h2>
            <p className="muted small">Wersje i ścieżki narzędzi wykrytych bezpośrednio na systemie.</p>
          </div>
        </div>
        {components.length === 0
          ? <p className="muted">{environmentLoading ? 'Pobieranie komponentów…' : 'Brak danych o komponentach.'}</p>
          : <div className="update-component-grid">
              {components.map((component) => <article key={component.name} className="update-component-card">
                <div className="update-component-heading">
                  <strong>{componentLabels[component.name] || component.name}</strong>
                  <span className="status-chip" data-ok={component.installed && component.state === 'available' ? 'true' : 'false'}>
                    {component.state === 'available' ? 'Dostępny' : component.state === 'missing' ? 'Brak' : 'Błąd'}
                  </span>
                </div>
                <small>{component.version || component.error || 'Brak informacji o wersji'}</small>
                <code>{component.path || '—'}</code>
              </article>)}
            </div>}
      </section>
    </>}

    {activeTab === 'advanced' && <>
      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Mechanizm aktualizacji</h2>
            <p className="muted small">Parametry rzeczywistego updatera systemd i instalatora.</p>
          </div>
        </div>
        <div className="update-safety-grid">
          <div><span>Repozytorium</span><strong>{status?.repository ?? '—'}</strong></div>
          <div><span>Gałąź</span><strong>{status?.ref ?? '—'}</strong></div>
          <div><span>Auto-update</span><strong>{status?.auto_update ? 'Włączony' : 'Wyłączony'}</strong></div>
          <div><span>Harmonogram</span><strong>{status?.schedule ?? '—'}</strong></div>
          <div><span>Równoległe aktualizacje</span><strong>Blokowane przez flock</strong></div>
          <div><span>Tryb timera</span><strong>Persistent</strong></div>
          <div><span>Healthcheck</span><strong>API + devbox doctor</strong></div>
          <div><span>Automatyczny rollback</span><strong>Brak — błąd zatrzymuje wdrożenie</strong></div>
        </div>
      </section>

      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Architektura instalacji</h2>
            <p className="muted small">Główne warstwy działającej instancji.</p>
          </div>
        </div>
        <div className="update-architecture">
          <article>
            <span>1 · UI</span>
            <strong>React / TypeScript</strong>
            <small>Statyczny build SPA.</small>
          </article>
          <div className="update-architecture-arrow">→</div>
          <article>
            <span>2 · Control plane</span>
            <strong>Go API</strong>
            <small>REST API, joby i orkiestracja.</small>
          </article>
          <div className="update-architecture-arrow">→</div>
          <article>
            <span>3 · Stan</span>
            <strong>SQLite</strong>
            <small>Lokalny stan i migracje DevBox.</small>
          </article>
          <div className="update-architecture-arrow">→</div>
          <article>
            <span>4 · Integracje</span>
            <strong>Docker · Nginx · SQL</strong>
            <small>Runtime aplikacji i infrastruktura.</small>
          </article>
        </div>
      </section>

      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Layout instalacji</h2>
            <p className="muted small">Domyślne ścieżki oficjalnego instalatora.</p>
          </div>
        </div>
        <div className="summary-grid update-layout-grid">
          <div><span>Usługa aplikacji</span><strong className="mono">devbox.service</strong></div>
          <div><span>Usługa aktualizacji</span><strong className="mono">devbox-update.service</strong></div>
          <div><span>Timer</span><strong className="mono">devbox-update.timer</strong></div>
          <div><span>API</span><strong className="mono">127.0.0.1:8787</strong></div>
          <div><span>Dane</span><strong className="mono">/var/lib/devbox</strong></div>
          <div><span>Konfiguracja</span><strong className="mono">/etc/devbox/devbox.env</strong></div>
          <div><span>Artefakty</span><strong className="mono">/opt/devbox</strong></div>
          <div><span>Binarki / helper</span><strong className="mono">/usr/local/lib/devbox</strong></div>
          <div><span>Log updatera</span><strong className="mono">/var/log/devbox-update.log</strong></div>
          <div><span>Log instalatora</span><strong className="mono">/var/log/devbox-installer.log</strong></div>
        </div>
      </section>
    </>}
  </>
}

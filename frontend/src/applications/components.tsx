import { useEffect, useMemo, useState } from 'react'
import { request } from '../api/client'
import type { RuntimeModuleOption } from '../api/types'
import { PHPModulePicker } from '../runtime/PHPModulePicker'
import type { Configuration, Detection } from './model'
import { statusNames } from './model'
import './styles.css'

export function ApplicationStatus({ value }: { value: string }) {
  const status = value.toLowerCase()
  const tone = ['running', 'healthy', 'success'].includes(status) ? 'good' : ['failed', 'unhealthy'].includes(status) ? 'bad' : ['degraded', 'starting', 'queued', 'waiting_for_configuration'].includes(status) ? 'warn' : 'neutral'
  return <span className={`acp-status acp-status-${tone}`} title={value}>{statusNames[status] ?? value}</span>
}
interface PHPModuleInventory {
  available: boolean
  modules: string[]
  message?: string
}

const phpStatusModules = ['pdo_mysql', 'mbstring', 'json', 'session', 'ctype', 'dom', 'fileinfo', 'filter', 'gd', 'iconv', 'pdo', 'mysqli', 'pdo_pgsql', 'pgsql', 'sqlite3', 'curl', 'intl', 'zip', 'bcmath', 'gmp', 'opcache', 'soap', 'ldap', 'redis', 'memcached', 'sockets', 'pcntl', 'exif', 'xdebug']

export function ConfigurationFields({ value = {}, driver = '', hasProvisionedWorkloads = false, applicationId, moduleContainerID, phpRuntime, onDriverChange, onRuntimeChange, onModulesChange }: { value?: Configuration; driver?: string; hasProvisionedWorkloads?: boolean; applicationId?: string; moduleContainerID?: string; phpRuntime?: boolean; onDriverChange?(driver: string): void; onRuntimeChange?(runtime: string): void; onModulesChange?(modules: string[]): void }) {
  const [catalog, setCatalog] = useState<RuntimeModuleOption[]>([])
  const [catalogError, setCatalogError] = useState('')
  const [moduleInventory, setModuleInventory] = useState<PHPModuleInventory | null>(null)
  const [moduleInventoryError, setModuleInventoryError] = useState('')
  const [moduleInventoryLoading, setModuleInventoryLoading] = useState(false)
  const runtime = typeof value.runtime === 'string' ? value.runtime : ''
  const showPHPStatus = phpRuntime ?? runtime === 'php'
  const selected = useMemo(() => new Set(Array.isArray(value.modules) ? value.modules.filter((item): item is string => typeof item === 'string') : []), [value.modules])
  const installedModuleSet = new Set((moduleInventory?.modules ?? []).map((item) => item.toLowerCase()))
  useEffect(() => {
    if (runtime !== 'php') { setCatalog([]); return }
    let cancelled = false
    setCatalogError('')
    request<RuntimeModuleOption[]>('/runtimes/php/modules').then((items) => { if (!cancelled) setCatalog(items ?? []) }).catch((reason: unknown) => {
      if (!cancelled) {
        setCatalog([])
        setCatalogError(reason instanceof Error ? reason.message : 'Nie udało się wczytać katalogu modułów PHP.')
      }
    })
    return () => { cancelled = true }
  }, [runtime])
  useEffect(() => {
    if (!applicationId || !showPHPStatus) {
      setModuleInventory(null)
      setModuleInventoryError('')
      return
    }
    if (!moduleContainerID) {
      setModuleInventory({ available: false, modules: [], message: 'Status modułów będzie dostępny po wdrożeniu kontenera PHP.' })
      return
    }
    let cancelled = false
    setModuleInventoryLoading(true)
    setModuleInventoryError('')
    request<PHPModuleInventory>(`/applications/${encodeURIComponent(applicationId)}/php-modules`)
      .then((result) => { if (!cancelled) setModuleInventory(result) })
      .catch((reason: unknown) => {
        if (!cancelled) {
          setModuleInventory(null)
          setModuleInventoryError(reason instanceof Error ? reason.message : 'Nie udało się sprawdzić modułów PHP.')
        }
      })
      .finally(() => { if (!cancelled) setModuleInventoryLoading(false) })
    return () => { cancelled = true }
  }, [applicationId, moduleContainerID, showPHPStatus])
  const text = (key: string) => typeof value[key] === 'string' || typeof value[key] === 'number' ? String(value[key]) : ''
  const env = value.environment && typeof value.environment === 'object' ? Object.entries(value.environment).map(([k, v]) => `${k}=${String(v)}`).join('\n') : ''
  return <>
    <div className="acp-fields">
      <label>Sterownik<select name="driver" value={driver} onChange={(event) => onDriverChange?.(event.target.value)}>
        <option value="">Wykryj automatycznie</option>
        <option value="managed">Samodzielny kontener — Managed runtime</option>
        <option value="dockerfile">Dockerfile</option>
        <option value="image">Gotowy obraz</option>
        <option value="compose">Docker Compose</option>
      </select><small>{hasProvisionedWorkloads ? 'Zmiana wymaga wdrożenia. DevBox zatrzyma bieżące usługi na czas przełączenia i przywróci je, jeśli nowe wdrożenie się nie powiedzie.' : 'Wybierz samodzielny kontener aplikacji albo Docker Compose z katalogu źródłowego.'}</small></label>
      <label>Runtime<select name="runtime" value={runtime} onChange={(event) => onRuntimeChange?.(event.target.value)}><option value="">Wykryj automatycznie</option>{['php', 'node', 'python', 'go', 'static'].map((name) => <option key={name}>{name}</option>)}</select><small>Aplikacja będzie uruchomiona w kontenerze.</small></label>
      <label>Wersja runtime<input name="runtime_version" defaultValue={text('runtime_version')} placeholder="Domyślna wersja runtime" /></label>
      <label>Port wewnętrzny<input name="container_port" type="number" min="0" max="65535" defaultValue={text('container_port')} placeholder="0 — automatycznie" /></label>
      <label>Port hosta<input name="host_port" type="number" min="0" max="65535" defaultValue={text('host_port')} placeholder="0 — automatycznie" /><small>Port aplikacji, nie port panelu DevBox.</small></label>
      <label>Protokół<select name="protocol" defaultValue={text('protocol')}><option value="">Wykryj automatycznie</option><option value="http">HTTP</option><option value="https">HTTPS (image / Compose)</option><option value="tcp">TCP (image / Compose)</option></select></label>
      <label>Healthcheck<input name="health_path" defaultValue={text('health_path')} placeholder="/" /><small>Ścieżka HTTP; nie pełny adres URL.</small></label>
      <label>Główny serwis Compose<input name="compose_service" defaultValue={text('compose_service')} placeholder="np. web" /><small>Ustaw przy kilku równorzędnych usługach HTTP.</small></label>
      {runtime === 'php' && <div className="acp-wide"><h3>Moduły PHP</h3><p className="muted">Wybierz rozszerzenia instalowane w obrazie kontenera PHP.</p>{applicationId && showPHPStatus && <section className="php-module-status" aria-label="Status modułów PHP w kontenerze">
        <h4>Moduły w uruchomionym kontenerze</h4>
        {moduleInventoryLoading && <p className="muted">Sprawdzanie modułów PHP…</p>}
        {moduleInventoryError && <p className="error-banner" role="alert">{moduleInventoryError}</p>}
        {!moduleInventoryLoading && !moduleInventoryError && moduleInventory && (moduleInventory.available
          ? <div className="php-module-status-list">{[...new Set([...phpStatusModules, ...catalog.map((item) => item.name), ...moduleInventory.modules])].sort().map((name) => {
            const available = installedModuleSet.has(name.toLowerCase())
            return <div className="php-module-status-row" key={name}><code>{name}</code><span>{available ? 'dostępne' : 'brak'}</span><strong data-state={available ? 'ok' : 'error'}>{available ? 'OK' : 'ERROR'}</strong></div>
          })}</div>
          : <p className="muted">{moduleInventory.message || 'Nie udało się odczytać modułów PHP z kontenera.'}</p>)}
      </section>}{catalogError && <p className="error-banner" role="alert">{catalogError}</p>}<PHPModulePicker catalog={catalog} selected={selected} loading={!catalog.length && !catalogError} onToggle={(name, enabled) => onModulesChange?.(enabled ? [...selected, name] : [...selected].filter((item) => item !== name))} onSelectionChange={(names) => onModulesChange?.(names)} /></div>}
      {runtime === 'php' && <input type="hidden" name="modules" value={[...selected].join(', ')} />}
    </div>
    <label>Publiczne zmienne środowiskowe<textarea name="environment" rows={4} defaultValue={env} placeholder={'APP_ENV=development\nTZ=Europe/Warsaw'} spellCheck={false} /><small>Hasła i tokeny dodaj po utworzeniu aplikacji w zakładce Sekrety. Nie zapisuj ich tutaj.</small></label>
  </>
}
export function DetectionResult({ value }: { value: Detection }) {
  return <section className="acp-card" aria-live="polite"><h2>Wynik analizy</h2><p>Sterownik: <strong>{value.driver || 'do wyboru'}</strong> · Pewność: {value.confidence}{value.runtime && ` · ${value.runtime} ${value.version ?? ''}`}</p>
    {value.requires_configuration && <p className="acp-notice">Potrzebna konfiguracja przed wdrożeniem. Wybierz serwis i port albo jawny sterownik.</p>}
    {value.services?.map((service) => <p key={service.name}><code>{service.name}</code> — {service.suggested_role}{service.primary ? ' · główny' : ''}</p>)}
    {value.endpoints?.map((ep) => <p key={`${ep.service}:${ep.container_port}`}><code>{ep.service}:{ep.container_port}</code> · {ep.protocol}{ep.primary ? ' · główny endpoint' : ''}</p>)}
    {value.warnings?.map((warning, i) => <p className="acp-notice" key={i}>{warning}</p>)}
    {value.reasons?.map((reason, i) => <small className="acp-line" key={i}>{reason}</small>)}
  </section>
}

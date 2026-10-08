import { useEffect, useMemo, useState } from 'react'
import { request } from '../api/client'
import type { RuntimeModuleOption } from '../api/types'
import { PHPModulePicker } from '../runtime/PHPModulePicker'
import { ComposeChoice } from './ComposeChoice'
import type { Configuration, Detection } from './model'
import { statusNames } from './model'
import { EnvironmentEditor } from './EnvironmentEditor'
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

export interface RuntimeChoice { name: string; label: string; versions: string[]; default_version: string; container_port: number; image_template?: string }

function initialRuntimeVersion(value: unknown, runtime: string): string {
  const version = String(value ?? '').trim()
  if (runtime !== 'php' || !version || /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(version)) return version
  const match = version.match(/(?:\^|~|>=?|<=?|=)?\s*(\d+\.\d+(?:\.\d+)?)/)
  return match?.[1] ?? ''
}

const phpStatusModules = ['pdo_mysql', 'mbstring', 'json', 'session', 'ctype', 'dom', 'fileinfo', 'filter', 'gd', 'iconv', 'pdo', 'mysqli', 'pdo_pgsql', 'pgsql', 'sqlite3', 'curl', 'intl', 'zip', 'bcmath', 'gmp', 'opcache', 'soap', 'ldap', 'redis', 'memcached', 'sockets', 'pcntl', 'exif', 'xdebug']

export function ConfigurationFields({ value = {}, deploymentMode = '', hasProvisionedWorkloads = false, applicationId, moduleContainerID, phpRuntime, onDeploymentModeChange, onRuntimeChange, onModulesChange, wizardStep }: { wizardStep?: number; value?: Configuration; deploymentMode?: string; hasProvisionedWorkloads?: boolean; applicationId?: string; moduleContainerID?: string; phpRuntime?: boolean; onDeploymentModeChange?(mode: string): void; onRuntimeChange?(runtime: string): void; onModulesChange?(modules: string[]): void }) {
  const [runtimeOptions, setRuntimeOptions] = useState<RuntimeChoice[]>([])
  const [runtimeError, setRuntimeError] = useState('')
  useEffect(() => { let cancelled = false; request<RuntimeChoice[]>('/runtimes/catalog').then((items) => { if (!cancelled) setRuntimeOptions(items) }).catch((error) => { if (!cancelled) setRuntimeError(String(error)) }); return () => { cancelled = true } }, [])
  const runtimeChoices = Object.fromEntries(runtimeOptions.map((item) => [item.name, item]))
  const [catalog, setCatalog] = useState<RuntimeModuleOption[]>([])
  const [catalogError, setCatalogError] = useState('')
  const [moduleInventory, setModuleInventory] = useState<PHPModuleInventory | null>(null)
  const [moduleInventoryError, setModuleInventoryError] = useState('')
  const [moduleInventoryLoading, setModuleInventoryLoading] = useState(false)
  const [runtime, setRuntime] = useState(typeof value.runtime === 'string' ? value.runtime : '')
  const [version, setVersion] = useState(() => initialRuntimeVersion(value.runtime_version, runtime))
  const [modules, setModules] = useState<string[]>(Array.isArray(value.modules) ? value.modules.filter((item): item is string => typeof item === 'string') : [])
  const versions = runtimeChoices[runtime]?.versions ?? []
  const [customVersion, setCustomVersion] = useState(false)
  const useCustomVersion = customVersion || (!!version && runtimeOptions.length > 0 && !versions.includes(version))
  function changeModules(names: string[]) { setModules(names); onModulesChange?.(names) }
  function changeRuntime(name: string) {
    setRuntime(name); setVersion(runtimeChoices[name]?.default_version ?? ''); setCustomVersion(false); setModules([]); setCatalog([]); setCatalogError('')
    onRuntimeChange?.(name); onModulesChange?.([])
  }
  const showPHPStatus = phpRuntime ?? runtime === 'php'
  const selected = useMemo(() => new Set(modules), [modules])
  const installedModuleSet = new Set((moduleInventory?.modules ?? []).map((item) => item.toLowerCase()))
  useEffect(() => {
    if (!runtime || runtime === 'static' || deploymentMode !== 'auto') { setCatalog([]); return }
    let cancelled = false
    setCatalogError('')
    request<RuntimeModuleOption[]>(`/runtimes/${encodeURIComponent(runtime)}/modules`).then((items) => { if (!cancelled) setCatalog(items ?? []) }).catch((reason: unknown) => {
      if (!cancelled) {
        setCatalog([])
        setCatalogError(reason instanceof Error ? reason.message : 'Nie udało się wczytać katalogu dodatków.')
      }
    })
    return () => { cancelled = true }
  }, [runtime, deploymentMode])
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

  return <>
    {runtimeError && <p role="alert">{runtimeError}</p>}
    <div className="acp-fields" data-step="1" hidden={wizardStep !== undefined && wizardStep !== 1}>
      <div className="acp-wide"><ComposeChoice value={deploymentMode} onChange={(value) => onDeploymentModeChange?.(value)} />
        <small>{hasProvisionedWorkloads ? 'Zmiana wymaga wdrożenia. DevBox zatrzyma bieżące usługi na czas przełączenia i przywróci je, jeśli nowe wdrożenie się nie powiedzie.' : 'Compose użyje pliku z katalogu głównego źródła. Ty wybierasz język, wersję i dodatki. DevBox zbuduje kontener i zamontuje katalog źródłowy do zapisu.'}</small>
      </div>
      {deploymentMode === 'auto' && <>
        <label>Język / oprogramowanie<select name="runtime" required value={runtime} onChange={(event) => changeRuntime(event.target.value)}><option value="">Wybierz język / oprogramowanie</option>{Object.entries(runtimeChoices).map(([name, choice]) => <option key={name} value={name}>{choice.label}</option>)}</select><small>Dla WordPress wybierz PHP. Profil WordPress zostanie rozpoznany ze źródła.</small></label>
        <label>Wersja {runtimeChoices[runtime]?.label ?? 'oprogramowania'}<select aria-label="Wybór wersji" required disabled={!runtime} value={useCustomVersion ? 'custom' : version} onChange={(event) => { const chosen = event.target.value; setCustomVersion(chosen === 'custom'); setVersion(chosen === 'custom' ? '' : chosen) }}><option value="">Wybierz wersję</option>{(version && !versions.includes(version) ? [version, ...versions] : versions).map((item) => <option key={item} value={item}>{item}</option>)}<option value="custom">Inna wersja — wpiszę ręcznie</option></select><small>{runtime === 'php' ? 'Wybierz wersję PHP dla obrazu kontenera. Ten wybór zastępuje zakres z composer.json.' : 'Propozycje wersji; dostępność konkretnego obrazu zostanie sprawdzona przy budowaniu kontenera.'}</small></label>
        {useCustomVersion ? <label>Dokładna wersja<input name="runtime_version" required disabled={!runtime} value={version} onChange={(event) => setVersion(event.target.value)} pattern="[A-Za-z0-9][A-Za-z0-9._\-]{0,63}" placeholder={runtime === 'php' ? 'np. 8.3.12' : 'np. 22.14.0'} /><small>Podaj numer wersji bazowego obrazu, bez nazwy obrazu i sufiksu dystrybucji.</small></label> : <input type="hidden" name="runtime_version" value={version} />}
      </>}
    </div><div data-step="2" hidden={wizardStep !== undefined && wizardStep !== 2}><div className="acp-fields">
      <input name="runtime_image" type="hidden" value={runtimeChoices[runtime]?.image_template?.replace('{version}', version) ?? ''} />
      <label>Port wewnętrzny<input name="container_port" type="number" min="0" max="65535" defaultValue={text('container_port')} placeholder="0 — automatycznie" /></label>
      <label>Port hosta<input name="host_port" type="number" min="0" max="65535" defaultValue={text('host_port')} placeholder="0 — automatycznie" /><small>Port aplikacji, nie port panelu DevBox.</small></label>
      <label className="acp-wide">Start command<input name="start_command" defaultValue={text('start_command')} placeholder="npm start / python app.py / apache2-foreground" /><small>Program i argumenty. Proces ma nasłuchiwać na 0.0.0.0 oraz wybranym porcie kontenera.</small></label>
      <label>Katalog roboczy w kontenerze<input name="working_directory" defaultValue={text('working_directory')} placeholder="Wykryj z obrazu / Dockerfile" /></label>
      <label>Restart policy<select name="restart_policy" defaultValue={text('restart_policy') || 'unless-stopped'}><option value="unless-stopped">unless-stopped</option><option value="always">always</option><option value="on-failure">on-failure</option><option value="no">no</option></select></label>
      {(deploymentMode === 'dockerfile' || deploymentMode === 'compose') && <label>Katalog montowania kodu<input name="mount_target" defaultValue={text('mount_target')} placeholder="Wykryj z WORKDIR, np. /srv/app" /></label>}
      {deploymentMode === 'auto' && runtime === 'php' && <label>Document root<input name="document_root" defaultValue={text('document_root')} placeholder="public lub . — wykryj automatycznie" /></label>}
      </div><details className="acp-card"><summary>Advanced — domena, SSL, healthcheck, moduły</summary><div className="acp-fields">
      <label>Domena (opcjonalna)<input name="domain" defaultValue={text('domain')} placeholder="myapp.local" /></label>
      <label>SSL<select name="tls_mode" defaultValue={text('tls_mode') || 'none'}><option value="none">Brak</option><option value="existing">Existing certificate</option><option value="letsencrypt" disabled>Let's Encrypt — infrastruktura ACME nie jest skonfigurowana</option></select></label>
      <label>Protokół<select name="protocol" defaultValue={text('protocol')}><option value="">Wykryj automatycznie</option><option value="http">HTTP</option><option value="https">HTTPS</option><option value="tcp">TCP</option></select></label>
      <label>Healthcheck<input name="health_path" defaultValue={text('health_path')} placeholder="/" /><small>Ścieżka HTTP; nie pełny adres URL.</small></label>
      {deploymentMode === 'compose' && <label>Główny serwis Compose<input name="compose_service" defaultValue={text('compose_service')} placeholder="np. web" /><small>Ustaw przy kilku równorzędnych usługach HTTP.</small></label>}
      {deploymentMode === 'auto' && runtime === 'php' && <div className="acp-wide"><h3>Moduły PHP</h3><p className="muted">Wybierz rozszerzenia instalowane w obrazie kontenera PHP.</p>{applicationId && showPHPStatus && <section className="php-module-status" aria-label="Status modułów PHP w kontenerze">
        <h4>Moduły w uruchomionym kontenerze</h4>
        {moduleInventoryLoading && <p className="muted">Sprawdzanie modułów PHP…</p>}
        {moduleInventoryError && <p className="error-banner" role="alert">{moduleInventoryError}</p>}
        {!moduleInventoryLoading && !moduleInventoryError && moduleInventory && (moduleInventory.available
          ? <div className="php-module-status-list">{[...new Set([...phpStatusModules, ...catalog.map((item) => item.name), ...moduleInventory.modules])].sort().map((name) => {
            const available = installedModuleSet.has(name.toLowerCase())
            return <div className="php-module-status-row" key={name}><code>{name}</code><span>{available ? 'dostępne' : 'brak'}</span><strong data-state={available ? 'ok' : 'error'}>{available ? 'OK' : 'ERROR'}</strong></div>
          })}</div>
          : <p className="muted">{moduleInventory.message || 'Nie udało się odczytać modułów PHP z kontenera.'}</p>)}
      </section>}{catalogError && <p className="error-banner" role="alert">{catalogError}</p>}<PHPModulePicker catalog={catalog} selected={selected} loading={!catalog.length && !catalogError} onToggle={(name, enabled) => changeModules(enabled ? [...selected, name] : [...selected].filter((item) => item !== name))} onSelectionChange={(names) => changeModules(names)} /></div>}
      {deploymentMode === 'auto' && runtime && !['php', 'static'].includes(runtime) && <div className="acp-wide"><h3>Dodatkowe oprogramowanie</h3>{catalogError && <p role="alert" className="error-banner">{catalogError}</p>}{!catalog.length && !catalogError && <p role="status">Wczytywanie dodatków…</p>}{catalog.map((item) => <label className="acp-check" key={item.name}><input type="checkbox" checked={selected.has(item.name)} onChange={(event) => changeModules(event.target.checked ? [...selected, item.name] : [...selected].filter((name) => name !== item.name))} />{item.label}<small>{item.description}</small></label>)}<small>Wersje bibliotek aplikacji określają jej pliki zależności.</small></div>}
      {deploymentMode === 'auto' && <input type="hidden" name="modules" value={[...selected].join(', ')} />}
    </div></details>
    <EnvironmentEditor value={value.environment && typeof value.environment === 'object' ? value.environment as Record<string, unknown> : {}} />
    </div>
  </>
}
export function DetectionResult({ value }: { value: Detection }) {
  return <section className="acp-card" aria-live="polite"><h2>Wynik analizy</h2><p>Tryb: <strong>{value.driver === 'compose' ? 'Docker Compose z aplikacji' : value.runtime === 'dockerfile' ? 'Dockerfile aplikacji' : value.runtime === 'image' ? 'Obraz OCI' : value.driver ? 'DevBox Auto Container' : 'do wyboru'}</strong> · Pewność: {value.confidence}{value.profile === 'wordpress' ? ' · WordPress (PHP + Apache)' : ''}{value.runtime && ` · ${value.runtime} ${value.version ?? ''}`}</p>
    {value.requires_configuration && <p className="acp-notice">{value.driver === 'compose' ? 'Potrzebna konfiguracja przed wdrożeniem. Wybierz główną usługę HTTP i jej port kontenera.' : 'Nie wykryto runtime aplikacji. Wybierz runtime albo popraw konfigurację źródła.'}</p>}
    {value.services?.map((service) => <p key={service.name}><code>{service.name}</code> — {service.suggested_role}{service.primary ? ' · główny' : ''}</p>)}
    {value.endpoints?.map((ep) => <p key={`${ep.service}:${ep.container_port}`}><code>{ep.service}:{ep.container_port}</code> · {ep.protocol}{ep.primary ? ' · główny endpoint' : ''}</p>)}
    {value.warnings?.map((warning, i) => <p className="acp-notice" key={i}>{warning}</p>)}
    {value.reasons?.map((reason, i) => <small className="acp-line" key={i}>{reason}</small>)}
  </section>
}

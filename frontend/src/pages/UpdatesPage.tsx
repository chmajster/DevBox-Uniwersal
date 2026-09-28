import { useCallback, useEffect, useState } from 'react'
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
import { UPDATE_STAGES, clampUpdatePercent, updateIsActive, updateStageState, updateStateLabel } from '../updates/progress'

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

export function UpdatesPage() {
  const [status,setStatus]=useState<UpdateStatus|null>(null)
  const [progress,setProgress]=useState<UpdateProgress|null>(null)
  const [systemInfo,setSystemInfo]=useState<SystemInfo|null>(null)
  const [platform,setPlatform]=useState<SystemPlatformInfo|null>(null)
  const [components,setComponents]=useState<SystemComponentStatus[]>([])
  const [docker,setDocker]=useState<DockerStatus|null>(null)
  const [mysql,setMySQL]=useState<MySQLStatus|null>(null)
  const [proxy,setProxy]=useState<ProxyStatus|null>(null)
  const [busy,setBusy]=useState(false)
  const [refreshing,setRefreshing]=useState(false)
  const [error,setError]=useState('')
  const [message,setMessage]=useState('')
  const [environmentWarning,setEnvironmentWarning]=useState('')

  const load=useCallback(async()=>{
    setError('')
    setEnvironmentWarning('')
    setRefreshing(true)
    try {
      const data=await request<UpdateStatus>('/update/status')
      setStatus(data)

      const [infoResult,platformResult,componentsResult,dockerResult,mysqlResult,proxyResult]=await Promise.allSettled([
        request<SystemInfo>('/system/info'),
        request<SystemPlatformInfo>('/system/platform'),
        request<SystemComponentStatus[]>('/system/components'),
        request<DockerStatus>('/docker/status'),
        request<MySQLStatus>('/mysql/status'),
        request<ProxyStatus>('/proxy/status')
      ])

      if(infoResult.status==='fulfilled')setSystemInfo(infoResult.value)
      if(platformResult.status==='fulfilled')setPlatform(platformResult.value)
      if(componentsResult.status==='fulfilled')setComponents(componentsResult.value)
      if(dockerResult.status==='fulfilled')setDocker(dockerResult.value)
      if(mysqlResult.status==='fulfilled')setMySQL(mysqlResult.value)
      if(proxyResult.status==='fulfilled')setProxy(proxyResult.value)

      const missing:string[]=[]
      if(infoResult.status==='rejected')missing.push('host')
      if(platformResult.status==='rejected')missing.push('platforma')
      if(componentsResult.status==='rejected')missing.push('komponenty')
      if(dockerResult.status==='rejected')missing.push('Docker')
      if(mysqlResult.status==='rejected')missing.push('MySQL')
      if(proxyResult.status==='rejected')missing.push('Nginx')
      if(missing.length)setEnvironmentWarning(`Nie udało się odczytać części informacji środowiska: ${missing.join(', ')}.`)
    } finally {
      setRefreshing(false)
    }
  },[])

  const loadProgress=useCallback(async()=>{
    const data=await request<UpdateProgress>('/update/progress')
    setProgress(data)
    return data
  },[])

  useEffect(()=>{
    load().catch(c=>setError(c instanceof Error?c.message:'Nie udało się sprawdzić aktualizacji'))
    loadProgress().catch(()=>undefined)
  },[load,loadProgress])

  useEffect(()=>{
    const timer=window.setInterval(()=>{loadProgress().catch(()=>undefined)},1500)
    return ()=>window.clearInterval(timer)
  },[loadProgress])

  useEffect(()=>{
    if(progress?.state==='succeeded'||progress?.state==='no_update'){
      load().catch(()=>undefined)
    }
  },[progress?.state,load])

  async function apply(){
    if(!window.confirm('Uruchomić aktualizację DevBox z repozytorium Git? Backend i frontend zostaną przebudowane, a devbox.service po wdrożeniu zostanie zrestartowany.'))return
    setBusy(true);setError('');setMessage('')
    try{
      const result=await request<{status:string;message:string}>('/update/apply',{method:'POST'})
      setMessage(result.message)
      setProgress({
        state:'starting',
        percent:1,
        stage:'starting',
        message:'Uruchamianie devbox-update.service…',
        current_version:status?.current_version,
        target_version:status?.latest_version,
        updated_at:new Date().toISOString()
      })
      window.setTimeout(()=>loadProgress().catch(()=>undefined),500)
    }catch(c){setError(c instanceof Error?c.message:'Nie udało się uruchomić aktualizacji')}
    finally{setBusy(false)}
  }

  const proxyOK=proxy ? proxy.detected&&proxy.config_valid : undefined
  const mysqlOK=mysql ? mysql.running : undefined
  const dockerOK=docker ? docker.available : undefined
  const apiOK=systemInfo ? true : undefined
  const progressPercent=clampUpdatePercent(progress?.percent)
  const progressActive=updateIsActive(progress)
  const progressFailed=progress?.state==='failed'
  const progressFinished=progress?.state==='succeeded'||progress?.state==='no_update'

  return <>
    <div className="page-heading">
      <div>
        <h1>Aktualizacje</h1>
        <p className="muted">Stan wersji, środowisko hosta, architektura instalacji i sposób działania automatycznego updatera.</p>
      </div>
      <button type="button" className="secondary" onClick={()=>load().catch(c=>setError(c instanceof Error?c.message:'Nie udało się sprawdzić aktualizacji'))} disabled={busy||refreshing}>
        <Icon name="refresh" size={16}/> {refreshing?'Sprawdzanie…':'Odśwież informacje'}
      </button>
    </div>
    {error&&<div className="error-banner">{error}</div>}
    {message&&<div className="success-banner">{message}</div>}
    {environmentWarning&&<div className="warning-banner">{environmentWarning}</div>}

    <section className="panel update-overview-panel">
      <div className="update-panel-heading">
        <div>
          <h2>Stan aktualizacji</h2>
          <p className="muted small">Porównanie aktualnie uruchomionego buildu z wybraną gałęzią repozytorium.</p>
        </div>
        <span className="status-chip" data-ok={statusAttribute(status ? !status.update_available : undefined)}>
          {status ? status.update_available?'Dostępna aktualizacja':'System aktualny' : 'Sprawdzanie'}
        </span>
      </div>

      <div className="summary-grid update-summary-grid">
        <div className="span-2"><span>Repozytorium</span><strong>{status?.repository??'—'}</strong></div>
        <div><span>Gałąź</span><strong>{status?.ref??'—'}</strong></div>
        <div><span>Ostatnie sprawdzenie</span><strong>{formatDate(status?.checked_at)}</strong></div>
        <div><span>Wersja zainstalowana</span><strong className="mono">{shortVersion(status?.current_version)}</strong></div>
        <div><span>Najnowszy commit</span><strong className="mono">{shortVersion(status?.latest_version)}</strong></div>
        <div><span>Data ostatniego commita</span><strong>{formatDate(status?.latest_commit_at)}</strong></div>
        <div><span>Auto-update</span><strong>{status?.auto_update?'Włączony':'Wyłączony / niedostępny'}</strong></div>
        <div><span>Harmonogram</span><strong>{status?.schedule??'—'}</strong></div>
      </div>

      {status?.last_error&&<div className="warning-banner">Nie udało się pobrać najnowszego commita: {status.last_error}</div>}
      <div className="form-actions update-actions">
        <button type="button" onClick={apply} disabled={busy||progressActive||!status?.update_available}>
          {busy?'Uruchamianie…':progressActive?'Aktualizacja trwa…':'Aktualizuj teraz'}
        </button>
        <span className="muted small">Ręczne uruchomienie startuje <code>devbox-update.service</code> w tle.</span>
      </div>
    </section>

    <section className={`panel update-progress-panel ${progressFailed?'is-failed':progressFinished?'is-complete':progressActive?'is-running':''}`} aria-live="polite">
      <div className="update-progress-heading">
        <div>
          <span className="eyebrow">POSTĘP AKTUALIZACJI</span>
          <h2>{progress?.message||'Brak aktywnej aktualizacji'}</h2>
          <p className="muted small">Stan jest odczytywany z procesu <code>devbox-update.service</code>. Podczas restartu API panel automatycznie wznowi odświeżanie.</p>
        </div>
        <div className="update-progress-value">
          <strong>{progressPercent}%</strong>
          <span className="status-chip" data-ok={progressFinished?'true':progressFailed?'false':undefined}>{updateStateLabel(progress)}</span>
        </div>
      </div>

      <div className="update-progress-bar">
        <progress max={100} value={progressPercent}>{progressPercent}%</progress>
        <div><span>0%</span><span>{progress?.stage&&progress.stage!=='idle'?progress.stage:'oczekiwanie'}</span><span>100%</span></div>
      </div>

      <div className="summary-grid update-progress-summary">
        <div><span>Zainstalowana wersja</span><strong className="mono">{shortVersion(progress?.current_version||status?.current_version)}</strong></div>
        <div><span>Wersja docelowa</span><strong className="mono">{shortVersion(progress?.target_version||status?.latest_version)}</strong></div>
        <div><span>Rozpoczęto</span><strong>{formatDate(progress?.started_at)}</strong></div>
        <div><span>Ostatnia zmiana</span><strong>{formatDate(progress?.updated_at)}</strong></div>
      </div>

      {progress?.error&&<div className="error-banner">{progress.error}</div>}
    </section>

    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Środowisko tej instancji</h2>
          <p className="muted small">Dane są odczytywane z aktualnego hosta, a nie wpisane na stałe w interfejsie.</p>
        </div>
      </div>

      <div className="update-environment-grid">
        <article className="update-info-card">
          <span className="update-card-label">Host</span>
          <strong>{systemInfo?.hostname||'—'}</strong>
          <small>{platform?.distro_name||systemInfo?.os||'System nieznany'}</small>
          <code>{platform?.arch||systemInfo?.arch||'—'}</code>
        </article>
        <article className="update-info-card">
          <span className="update-card-label">Platforma</span>
          <strong>{platformLabel(platform,systemInfo)}</strong>
          <small>{platform?.systemd?'systemd aktywny':'systemd niedostępny lub nieaktywny'}</small>
          <code>{platform?.wsl?`WSL${platform.wsl_version||''}`:'Linux host'}</code>
        </article>
        <article className="update-info-card">
          <span className="update-card-label">Backend / API</span>
          <strong>Go · devbox.service</strong>
          <small>{systemInfo?.go_version||'Wersja Go niedostępna'}</small>
          <code>{shortVersion(systemInfo?.version||status?.current_version)}</code>
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
          <small>Konfiguracja, użytkownicy, projekty, joby i metadane DevBox</small>
          <code>devbox.db</code>
        </article>
        <article className="update-info-card">
          <span className="update-card-label">Provisioning baz</span>
          <strong>MySQL / MariaDB</strong>
          <small>{mysql?.version||mysql?.connection_state||'Stan niedostępny'}</small>
          <code>{mysql?.running?'running':'not running'}</code>
        </article>
      </div>

      <div className="update-service-grid">
        <div className="update-service-row">
          <div><strong>DevBox API</strong><small>{systemInfo?.go_version||'brak danych'}</small></div>
          <span className="status-chip" data-ok={statusAttribute(apiOK)}>{apiOK?'Działa':'Brak danych'}</span>
        </div>
        <div className="update-service-row">
          <div><strong>Docker Engine</strong><small>{docker?.server_version||docker?.client_version||docker?.error||'brak danych'}</small></div>
          <span className="status-chip" data-ok={statusAttribute(dockerOK)}>{dockerOK===undefined?'Brak danych':dockerOK?'Działa':'Niedostępny'}</span>
        </div>
        <div className="update-service-row">
          <div><strong>Nginx</strong><small>{proxy?.version||proxy?.error||'brak danych'}</small></div>
          <span className="status-chip" data-ok={statusAttribute(proxyOK)}>{proxyOK===undefined?'Brak danych':proxyOK?'Konfiguracja OK':'Problem'}</span>
        </div>
        <div className="update-service-row">
          <div><strong>MySQL / MariaDB</strong><small>{mysql?.version||mysql?.connection_state||'brak danych'}</small></div>
          <span className="status-chip" data-ok={statusAttribute(mysqlOK)}>{mysqlOK===undefined?'Brak danych':mysqlOK?'Działa':'Niedostępny'}</span>
        </div>
      </div>
    </section>

    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Na czym stoi DevBox</h2>
          <p className="muted small">Przepływ głównych warstw działającej instalacji.</p>
        </div>
      </div>
      <div className="update-architecture">
        <article>
          <span>1 · UI</span>
          <strong>React / TypeScript</strong>
          <small>SPA zbudowane przez npm i serwowane jako statyczne artefakty.</small>
        </article>
        <div className="update-architecture-arrow">→</div>
        <article>
          <span>2 · Control plane</span>
          <strong>Go API</strong>
          <small>Proces <code>devbox.service</code>, REST API, joby i orkiestracja.</small>
        </article>
        <div className="update-architecture-arrow">→</div>
        <article>
          <span>3 · Stan</span>
          <strong>SQLite</strong>
          <small>Lokalny stan control plane oraz migracje bazy DevBox.</small>
        </article>
        <div className="update-architecture-arrow">→</div>
        <article>
          <span>4 · Integracje</span>
          <strong>Docker · Nginx · MySQL</strong>
          <small>Uruchamianie aplikacji, reverse proxy i provisioning baz.</small>
        </article>
      </div>
    </section>

    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Jak przebiega aktualizacja</h2>
          <p className="muted small">Rzeczywisty przepływ wynikający z <code>devbox-updater</code> i <code>install.sh --update</code>.</p>
        </div>
      </div>
      <ol className="update-pipeline update-pipeline-live">
        {UPDATE_STAGES.map((stage,index)=>{
          const state=updateStageState(progress,stage.id)
          return <li key={stage.id} data-state={state}>
            <span>{state==='done'?<Icon name="check" size={15}/>:index+1}</span>
            <div>
              <div className="update-stage-title"><strong>{stage.label}</strong><small>{stage.percent}%</small></div>
              <small>{stage.description}</small>
            </div>
          </li>
        })}
      </ol>

      <div className="update-safety-grid">
        <div><span>Równoległe aktualizacje</span><strong>Blokowane przez flock</strong></div>
        <div><span>Uprawnienia</span><strong>Ograniczony helper + systemd oneshot</strong></div>
        <div><span>Timer</span><strong>12 h + losowe opóźnienie do 10 min</strong></div>
        <div><span>Po restarcie</span><strong>Healthcheck API + doctor</strong></div>
        <div><span>Tryb timera</span><strong>Persistent</strong></div>
        <div><span>Automatyczny rollback</span><strong>Brak — błąd zatrzymuje wdrożenie</strong></div>
      </div>
    </section>

    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Komponenty hosta</h2>
          <p className="muted small">Wersje i ścieżki narzędzi wykrytych bezpośrednio na systemie.</p>
        </div>
      </div>
      {components.length===0
        ? <p className="muted">Brak danych o komponentach.</p>
        : <div className="update-component-grid">
            {components.map(component=><article key={component.name} className="update-component-card">
              <div className="update-component-heading">
                <strong>{componentLabels[component.name]||component.name}</strong>
                <span className="status-chip" data-ok={component.installed&&component.state==='available'?'true':'false'}>
                  {component.state==='available'?'Dostępny':component.state==='missing'?'Brak':'Błąd'}
                </span>
              </div>
              <small>{component.version||component.error||'Brak informacji o wersji'}</small>
              <code>{component.path||'—'}</code>
            </article>)}
          </div>}
    </section>

    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Layout instalacji</h2>
          <p className="muted small">Domyślne ścieżki używane przez oficjalny <code>install.sh</code>; mogą zostać nadpisane zmiennymi <code>DEVBOX_*</code>.</p>
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
  </>
}

import { useCallback } from 'react'
import { request } from '../api/client'
import type { ApplicationDatabaseBinding } from '../api/types'
import { Icon } from '../components/Icon'
import { usePolling } from '../control-room/usePolling'
import { ApplicationStatus } from './components'
import { CopyButton } from './CopyButton'
import { runtimeLabel, type ApplicationTab } from './detailModel'
import { date, deploymentModeName, sourceNames, type ApplicationDetail } from './model'

function DatabaseSummary({ id, onOpen }: { id: string; onOpen(): void }) {
  const load = useCallback((signal: AbortSignal) => request<{ binding: ApplicationDatabaseBinding; configured: boolean }>(`/applications/${encodeURIComponent(id)}/database-binding`, { signal }), [id])
  const state = usePolling(load, 10000)
  const binding = state.data?.binding
  return <section className="acp-card app-summary-card">
    <div className="app-section-title"><Icon name="database" /><h2>Baza danych</h2></div>
    {state.error ? <p role="alert" className="error-banner">{state.error}</p> : !state.data ? <p className="muted">Wczytywanie połączenia…</p> : binding?.database_id ? <><strong className="app-summary-value">{binding.database_name}</strong><p className="muted">{binding.engine} · konto {binding.username}</p></> : <><strong className="app-summary-value">Bez przypisanej bazy</strong><p className="muted">Połącz aplikację z MySQL, MariaDB lub PostgreSQL.</p></>}
    <button className="secondary-button" onClick={onOpen}>Szczegóły połączenia<Icon name="arrow" size={15} /></button>
  </section>
}

export function ApplicationOverview({ app, url, inspectionFailed, onTab }: { app: ApplicationDetail; url: string | null; inspectionFailed: boolean; onTab(tab: ApplicationTab): void }) {
  const primary = app.endpoints.find((item) => item.primary)
  const latest = app.deployments[0]
  const source = app.source.local_path || app.source.repository_url || app.source.docker_image || 'Źródło zarządzane przez DevBox'
  const httpEndpoint = primary && ['http', 'https'].includes(primary.protocol)
  return <>
    <div className="app-overview-metrics">
      <div className="app-overview-metric"><span><Icon name="box" size={16} />Stan kontenera</span><ApplicationStatus value={inspectionFailed ? 'unknown' : app.observed_state} /><small>{app.workloads.length} {app.workloads.length === 1 ? 'usługa' : 'usług'} w aplikacji</small></div>
      <div className="app-overview-metric"><span><Icon name="activity" size={16} />Dostęp HTTP</span><strong className={url ? 'app-text-success' : ''}>{inspectionFailed ? 'Nieznany' : url ? 'Dostępna' : httpEndpoint ? 'Niedostępna' : 'Brak endpointu HTTP'}</strong><small>{inspectionFailed ? 'Nie udało się odczytać stanu' : httpEndpoint ? `Port kontenera ${primary.container_port}` : 'Sprawdź porty i konfigurację'}</small></div>
      <div className="app-overview-metric"><span><Icon name="code" size={16} />Środowisko</span><strong>{runtimeLabel(app)}</strong><small>{deploymentModeName(app.source_config?.deployment_mode, app.driver)}</small></div>
      <div className="app-overview-metric"><span><Icon name="network" size={16} />Port hosta</span><strong>{primary?.host_port ?? 'Nie przydzielono'}</strong><small>{primary ? `${primary.protocol.toUpperCase()} · port kontenera ${primary.container_port}` : 'Zostanie przydzielony przy wdrożeniu'}</small></div>
    </div>
    <div className="app-overview-grid">
      <div className="app-overview-main">
        <section className={`acp-card app-endpoint-hero ${url ? 'is-available' : ''}`}>
          <div className="app-section-title"><Icon name="globe" /><h2>Adres aplikacji</h2>{url && <span className="app-live-label"><span className="status-dot" />HTTP dostępne</span>}</div>
          {url ? <><a className="app-endpoint-address" href={url} target="_blank" rel="noreferrer">{url}<Icon name="external" size={20} /></a><div className="app-endpoint-footer"><span>{primary?.domain && primary.route_active ? 'Domena kieruje do aplikacji' : 'Opublikowany port na tym serwerze'}</span><CopyButton key={url} value={url} /></div></> : <div className="app-empty-inline"><Icon name="globe" size={28} /><div><strong>{inspectionFailed ? 'Adres wymaga ponownego sprawdzenia' : 'Brak aktywnego adresu HTTP'}</strong><p>{inspectionFailed ? 'Odśwież stan, aby sprawdzić dostępność aplikacji.' : 'Uruchom lub wdróż aplikację, aby udostępnić jej adres.'}</p></div></div>}
        </section>
        <section className="acp-card app-source-card">
          <div className="app-section-title"><Icon name={app.source_type === 'git' ? 'branch' : 'code'} /><h2>Źródło aplikacji</h2><span className="app-subtle-label">{sourceNames[app.source_type] ?? app.source_type}</span></div>
          <code className="app-source-path">{source}</code>
          <div className="app-source-footer"><span>{app.source_type === 'docker_image' ? 'Obraz uruchamiany bez montowania kodu' : 'Pliki i konfiguracja montowania są w zakładce Środowisko'}</span><button className="app-text-button secondary-button" onClick={() => onTab('Runtime')}>Zobacz środowisko<Icon name="arrow" size={15} /></button></div>
        </section>
      </div>
      <aside className="app-overview-aside" aria-label="Połączenia i historia">
        <DatabaseSummary id={app.id} onOpen={() => onTab('Database')} />
        <section className="acp-card app-summary-card">
          <div className="app-section-title"><Icon name="clock" /><h2>Ostatnie wdrożenie</h2></div>
          {latest ? <><ApplicationStatus value={latest.status} /><p className="muted">{date(latest.finished_at || latest.created_at)}</p><p className="app-deployment-hint">{latest.status === 'failed' ? 'Nieudane wdrożenie. Bieżący stan aplikacji jest sprawdzany osobno.' : `Etap: ${latest.stage}`}</p></> : <p className="muted">Historia pojawi się po pierwszym wdrożeniu.</p>}
          <button className="secondary-button" onClick={() => onTab('Jobs')}>Zobacz historię<Icon name="arrow" size={15} /></button>
        </section>
      </aside>
    </div>
  </>
}

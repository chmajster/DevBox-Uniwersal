import { apiURL } from '../api/client'
import { Icon } from '../components/Icon'
import { ApplicationStatus } from './components'
import { CopyButton } from './CopyButton'
import { activeRuntimeMetadata, runtimeLabel } from './detailModel'
import { deploymentModeName, endpointURL, type ApplicationDetail } from './model'

export function ApplicationRuntime({ app, editable }: { app: ApplicationDetail; editable: boolean }) {
  const metadata = activeRuntimeMetadata(app)
  const primary = app.endpoints.find((item) => item.primary)
  const networks = Array.isArray(metadata.networks) ? metadata.networks.filter((item): item is string => typeof item === 'string') : typeof metadata.network === 'string' ? [metadata.network] : []
  const volumes = Array.isArray(metadata.volumes) ? metadata.volumes as { type?: string; source?: string; target?: string; read_only?: boolean }[] : []
  return <>
    <section className="acp-card">
      <div className="app-section-title"><Icon name="code" /><h2>Środowisko uruchomieniowe</h2>{editable && app.driver === 'managed' && <a className="button-link secondary-button app-section-action" href={apiURL(`/applications/${encodeURIComponent(app.id)}/export`)} download><Icon name="archive" size={15} />Eksport Dockerfile i Compose</a>}</div>
      <dl className="acp-facts app-runtime-facts">
        <div><dt>Technologia i wersja</dt><dd>{runtimeLabel(app)}</dd></div>
        <div><dt>Tryb wdrożenia</dt><dd>{deploymentModeName(app.source_config?.deployment_mode, app.driver)}</dd></div>
        <div><dt>Konfiguracja generowana</dt><dd>{typeof metadata.generated === 'boolean' ? metadata.generated ? 'Tak' : 'Nie' : 'Brak informacji w zapisanym runtime'}</dd></div>
        <div className="acp-wide"><dt>Kontener główny</dt><dd><code>{String(metadata.container_name ?? app.workloads.find((item) => item.primary)?.driver_resource_id ?? 'Nie utworzono')}</code></dd></div>
        <div><dt>Port kontenera → port hosta</dt><dd>{primary ? `${primary.container_port} → ${primary.host_port ?? 'Nie przydzielono'}` : 'Nie przydzielono'}</dd></div>
        <div><dt>Restart kontenera</dt><dd>{String(app.source_config?.restart_policy || 'Domyślna polityka obrazu')}</dd></div>
        <div><dt>Komenda uruchomienia</dt><dd><code>{String(app.source_config?.start_command || 'Domyślna komenda runtime')}</code></dd></div>
        <div className="acp-wide"><dt>Sieci z zapisanego runtime</dt><dd>{networks.length ? networks.map((network) => <code className="app-code-chip" key={network}>{network}</code>) : 'Brak informacji o sieciach'}</dd></div>
      </dl>
    </section>
    <section className="acp-card">
      <div className="app-section-title"><Icon name="disk" /><h2>Pliki i wolumeny</h2></div>
      {volumes.length ? <div className="app-mount-list">{volumes.map((volume, index) => <div className="app-mount" key={`${volume.target}:${index}`}><span className="app-code-chip">{volume.type === 'bind' ? 'Katalog lokalny' : volume.type || 'Wolumen'}</span><code>{volume.source || 'Wolumen zarządzany'}</code><Icon name="arrow" size={16} /><code>{volume.target || 'Nie podano'}</code><span className="app-subtle-label">{volume.read_only ? 'Tylko odczyt' : 'Odczyt i zapis'}</span></div>)}</div> : <p className="muted">{app.source_type === 'docker_image' ? 'Obraz OCI działa bez montowania katalogu źródłowego.' : 'Ten zapis runtime nie zawiera informacji o mountach. Katalog źródłowy jest pokazany w Przeglądzie; pełne dane zapisuje kolejne udane wdrożenie.'}</p>}
    </section>
  </>
}

export function ApplicationEndpoints({ app, inspectionFailed, domains = false }: { app: ApplicationDetail; inspectionFailed: boolean; domains?: boolean }) {
  return <section className="acp-card">
    <div className="app-section-title"><Icon name={domains ? 'globe' : 'network'} /><h2>{domains ? 'Domeny i dostęp HTTPS' : 'Porty i adresy'}</h2></div>
    {domains && <p className="muted">Domena kieruje przez reverse proxy na port aplikacji. Ustaw DNS lub wpis w pliku hosts na adres serwera; certyfikat TLS konfigurujesz w Ustawieniach.</p>}
    <div className="app-endpoint-list">{app.endpoints.map((endpoint) => {
      const link = inspectionFailed ? null : endpointURL(endpoint, window.location.hostname)
      return <article className="app-endpoint-item" key={endpoint.id}>
        <div className="app-endpoint-title"><strong>{endpoint.name}</strong>{endpoint.primary && <span className="app-code-chip">Główny</span>}<ApplicationStatus value={inspectionFailed ? 'unknown' : endpoint.status} /></div>
        <dl className="acp-facts"><div><dt>Protokół</dt><dd>{endpoint.protocol.toUpperCase()}</dd></div><div><dt>Port kontenera</dt><dd>{endpoint.container_port}</dd></div><div><dt>Port hosta</dt><dd>{endpoint.host_port ?? 'Nie przydzielono'}</dd></div></dl>
        {domains && <dl className="acp-facts"><div><dt>Domena</dt><dd>{endpoint.domain || 'Nie przypisano domeny'}</dd></div><div><dt>Reverse proxy</dt><dd>{endpoint.route_active ? 'Trasa aktywna' : 'Trasa nieaktywna'}</dd></div><div><dt>Certyfikat</dt><dd>{endpoint.tls_mode === 'existing' ? 'Własny certyfikat' : 'Brak TLS'}</dd></div></dl>}
        <div className="app-endpoint-link">{link ? <><a href={link} target="_blank" rel="noreferrer">{link}<Icon name="external" size={14} /></a><CopyButton key={link} value={link} /></> : <span className="muted">{inspectionFailed ? 'Dostępność adresu nie została potwierdzona.' : 'Brak aktywnego adresu HTTP.'}</span>}</div>
      </article>
    })}{!app.endpoints.length && <div className="app-empty-inline"><Icon name="network" size={28} /><p>Porty pojawią się po pierwszym wdrożeniu aplikacji.</p></div>}</div>
  </section>
}

export function ApplicationContainers({ app, inspectionFailed }: { app: ApplicationDetail; inspectionFailed: boolean }) {
  return <section className="acp-card"><div className="app-section-title"><Icon name="box" /><h2>Usługi aplikacji</h2><span className="app-code-chip">{app.workloads.length}</span></div>
    <div className="app-container-list">{app.workloads.map((workload) => <article className="app-container" key={workload.id}>
      <div className="app-endpoint-title"><Icon name="box" /><strong>{workload.name}</strong>{workload.primary && <span className="app-code-chip">Główna usługa</span>}<ApplicationStatus value={inspectionFailed ? 'unknown' : workload.observed_state} /></div>
      <dl className="acp-facts"><div><dt>Rola</dt><dd>{workload.role}</dd></div><div><dt>Sprawność</dt><dd><ApplicationStatus value={inspectionFailed ? 'unknown' : workload.health_state} /></dd></div><div className="acp-wide"><dt>Obraz</dt><dd><code>{workload.image || 'Nie zapisano obrazu'}</code></dd></div><div className="acp-wide"><dt>Identyfikator kontenera</dt><dd><code>{workload.driver_resource_id || 'Brak kontenera'}</code></dd></div></dl>
    </article>)}{!app.workloads.length && <div className="app-empty-inline"><Icon name="box" size={28} /><div><strong>Brak utworzonych usług</strong><p>Wdróż aplikację, aby zbudować i uruchomić kontenery.</p></div></div>}</div>
  </section>
}

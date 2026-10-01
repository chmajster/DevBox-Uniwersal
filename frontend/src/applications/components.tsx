import type { Configuration, Detection } from './model'
import { statusNames } from './model'
import './styles.css'

export function ApplicationStatus({ value }: { value: string }) {
  const status = value.toLowerCase()
  const tone = ['running', 'healthy', 'success'].includes(status) ? 'good' : ['failed', 'unhealthy'].includes(status) ? 'bad' : ['degraded', 'starting', 'queued', 'waiting_for_configuration'].includes(status) ? 'warn' : 'neutral'
  return <span className={`acp-status acp-status-${tone}`} title={value}>{statusNames[status] ?? value}</span>
}
export function ConfigurationFields({ value = {} }: { value?: Configuration }) {
  const text = (key: string) => typeof value[key] === 'string' || typeof value[key] === 'number' ? String(value[key]) : ''
  const env = value.environment && typeof value.environment === 'object' ? Object.entries(value.environment).map(([k, v]) => `${k}=${String(v)}`).join('\n') : ''
  return <>
    <div className="acp-fields">
      <label>Runtime<select name="runtime" defaultValue={text('runtime')}><option value="">Wykryj automatycznie</option>{['php', 'node', 'python', 'go', 'static'].map((name) => <option key={name}>{name}</option>)}</select><small>Dotyczy sterownika managed.</small></label>
      <label>Wersja runtime<input name="runtime_version" defaultValue={text('runtime_version')} placeholder="Domyślna wersja runtime" /></label>
      <label>Port wewnętrzny<input name="container_port" type="number" min="0" max="65535" defaultValue={text('container_port')} placeholder="0 — automatycznie" /></label>
      <label>Port hosta<input name="host_port" type="number" min="0" max="65535" defaultValue={text('host_port')} placeholder="0 — automatycznie" /><small>Port aplikacji, nie port panelu DevBox.</small></label>
      <label>Protokół<select name="protocol" defaultValue={text('protocol')}><option value="">Wykryj automatycznie</option><option value="http">HTTP</option><option value="https">HTTPS (image / Compose)</option><option value="tcp">TCP (image / Compose)</option></select></label>
      <label>Healthcheck<input name="health_path" defaultValue={text('health_path')} placeholder="/" /><small>Ścieżka HTTP; nie pełny adres URL.</small></label>
      <label>Główny serwis Compose<input name="compose_service" defaultValue={text('compose_service')} placeholder="np. web" /><small>Ustaw przy kilku równorzędnych usługach HTTP.</small></label>
      <label>Moduły runtime<input name="modules" defaultValue={Array.isArray(value.modules) ? value.modules.join(', ') : ''} placeholder="np. gd, zip, pdo_mysql" /></label>
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

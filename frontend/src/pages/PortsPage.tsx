import { useCallback } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { PortRecord } from '../api/types'
import { usePolling } from '../control-room/usePolling'

export function PortsPage() {
  const load = useCallback((signal: AbortSignal) => request<PortRecord[]>('/ports', { signal }), [])
  const ports = usePolling(load, 5000)
  return <><div className="page-heading"><div><h1>Porty</h1><p className="muted">Trwałe rezerwacje portów hosta i rzeczywiste mapowania Docker. Wybierz port lub Auto w ustawieniach aplikacji.</p></div><button className="secondary-button" onClick={ports.refresh}>Odśwież</button></div>
    {ports.error && <p role="alert" className="error-banner">{ports.error}</p>}
    <div className="table-wrap"><table><thead><tr><th>Host port</th><th>Aplikacja</th><th>Container port</th><th>Rezerwacja</th><th>Mapping</th><th>Socket hosta</th><th>Settings</th></tr></thead><tbody>{(ports.data ?? []).map((port) => <tr key={port.id}><td><strong>{port.port}</strong></td><td>{port.application ?? port.application_id ?? port.project_id ?? '—'}</td><td>{port.container_port ?? '—'}</td><td>{port.state}</td><td>{port.mapping_active ? 'Aktywny' : 'Nieaktywny'}</td><td>{port.socket_available ? 'Wolny' : 'Zajęty'}</td><td>{port.application_id && <Link to={`/apps/${encodeURIComponent(port.application_id)}?tab=Settings`}>Konfiguracja aplikacji</Link>}</td></tr>)}{ports.data?.length === 0 && <tr><td colSpan={7}>Brak rezerwacji.</td></tr>}</tbody></table></div>
  </>
}

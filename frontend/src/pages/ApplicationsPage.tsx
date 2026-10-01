import { useState } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { usePolling } from '../control-room/usePolling'
import { ApplicationStatus } from '../applications/components'
import { endpointURL, sourceNames, type Application } from '../applications/model'

const load = (signal: AbortSignal) => request<Application[]>('/applications', { signal })
export function ApplicationsPage() {
  const { user } = useAuth()
  const { data, error, updatedAt, refresh } = usePolling(load, 5000)
  const [query, setQuery] = useState('')
  const items = (data ?? []).filter((app) => `${app.name} ${app.description} ${app.driver}`.toLocaleLowerCase('pl').includes(query.toLocaleLowerCase('pl')))
  return <div className="acp"><header className="acp-header"><div><h1>Aplikacje</h1><p>Źródła, usługi i endpointy w jednym miejscu.</p></div><div className="acp-actions"><button className="secondary-button" onClick={refresh}>Odśwież</button>{user?.role !== 'viewer' && <Link className="button-link" to="/apps/new">Dodaj aplikację</Link>}</div></header>
    <div className="acp-toolbar"><label>Szukaj aplikacji<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Nazwa, opis lub sterownik" /></label><small>Odczyt: {updatedAt ? new Date(updatedAt).toLocaleTimeString('pl-PL') : '—'} · odświeżanie co 5 s</small></div>
    {error && <p role="alert" className="error-banner">{error}</p>}
    {!data && !error && <p role="status">Wczytywanie aplikacji…</p>}
    {data?.length === 0 && <section className="acp-card"><h2>Brak aplikacji</h2><p>Dodaj repozytorium Git, katalog na hoście, gotowy obraz kontenera lub pustą aplikację. Dotychczasowe rekordy Project nie są automatycznie adoptowane.</p></section>}
    {!!data?.length && <div className="acp-table"><table><thead><tr><th>Aplikacja</th><th>Stan rzeczywisty</th><th>Sterownik / runtime</th><th>Usługi</th><th>Endpoint</th><th>Operacja / ostatni deploy</th></tr></thead><tbody>{items.map((app) => { const url = endpointURL(app.primary_endpoint, window.location.hostname); return <tr key={app.id}>
      <td><Link to={`/apps/${encodeURIComponent(app.id)}`}><strong>{app.name}</strong></Link><small className="acp-line">{sourceNames[app.source_type]}</small></td>
      <td><ApplicationStatus value={app.status} /><small className="acp-line">Docelowo: {app.desired_state === 'running' ? 'uruchomiona' : 'zatrzymana'}</small></td>
      <td>{app.driver || 'Automatycznie'}<small className="acp-line">{app.runtime ? `${app.runtime.name} ${app.runtime.version ?? ''}` : '—'}</small></td><td>{app.workload_count ?? 0}</td>
      <td>{url ? <a href={url} target="_blank" rel="noreferrer">{app.primary_endpoint?.host_port} ↗</a> : '—'}</td>
      <td>{app.active_operation ? <Link to={`/jobs?job=${encodeURIComponent(app.active_operation.id)}`}>{app.active_operation.type} · {app.active_operation.status}</Link> : app.last_deployment ? <ApplicationStatus value={app.last_deployment.status} /> : 'Brak wdrożenia'}</td>
    </tr> })}{items.length === 0 && <tr><td colSpan={6}>Brak pasujących aplikacji.</td></tr>}</tbody></table></div>}
  </div>
}

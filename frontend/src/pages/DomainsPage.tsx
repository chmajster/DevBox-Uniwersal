import { useCallback, useState } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { DomainRecord, ProxyStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { usePolling } from '../control-room/usePolling'

export function DomainsPage() {
  const { user } = useAuth()
  const [notice, setNotice] = useState('')
  const [error, setError] = useState('')
  const load = useCallback(async (signal: AbortSignal) => {
    const [domains, proxy] = await Promise.all([request<DomainRecord[]>('/domains', { signal }), request<ProxyStatus>('/proxy/status', { signal })])
    return { domains, proxy }
  }, [])
  const state = usePolling(load, 5000)
  async function reload() {
    setError(''); setNotice('')
    try { await request('/proxy/reload', { method: 'POST', body: '{}' }); setNotice('Konfiguracja zweryfikowana, Nginx przeładowany.'); state.refresh() }
    catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)) }
  }
  return <><div className="page-heading"><div><h1>Domeny i SSL</h1><p className="muted">Domenę i SSL ustawiasz w aplikacji. DevBox stosuje konfigurację proxy po poprawnej walidacji. DNS lub wpis hosts musi wskazywać serwer DevBox.</p></div><div className="toolbar"><span>{state.data?.proxy.detected && state.data.proxy.config_valid ? 'Nginx OK' : 'Nginx ERROR / brak'}</span><button className="secondary-button" onClick={state.refresh}>Odśwież</button>{user?.role === 'admin' && <button className="secondary-button" onClick={() => void reload()}>Reload Nginx</button>}</div></div>
    {(error || state.error || state.data?.proxy.error) && <p role="alert" className="error-banner">{error || state.error || state.data?.proxy.error}</p>}{notice && <p role="status">{notice}</p>}
    <div className="table-wrap"><table><thead><tr><th>Domena</th><th>Aplikacja</th><th>Upstream HTTP</th><th>SSL</th><th>Stan</th><th>Settings</th></tr></thead><tbody>{(state.data?.domains ?? []).map((domain) => <tr key={domain.id}><td>{domain.status === 'active' ? <a href={`${domain.tls_enabled ? 'https' : 'http'}://${domain.hostname}`} target="_blank" rel="noreferrer">{domain.hostname}</a> : domain.hostname}</td><td>{domain.application || domain.project_id}</td><td>{domain.target || `127.0.0.1:${domain.target_port}`}</td><td>{domain.tls_enabled ? 'Existing certificate' : 'Brak'}</td><td>{domain.status}</td><td><Link to={`/apps/${encodeURIComponent(domain.project_id)}?tab=Settings`}>Konfiguracja aplikacji</Link></td></tr>)}{state.data?.domains.length === 0 && <tr><td colSpan={6}>Brak domen.</td></tr>}</tbody></table></div>
  </>
}

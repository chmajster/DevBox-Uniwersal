import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { request } from '../api/client'
import type { DomainMutationResult, DomainRecord, ProxyStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { DomainTLS } from '../components/DomainTLS'

export function DomainsPage() {
  const { user } = useAuth()
  const [domains, setDomains] = useState<DomainRecord[]>([])
  const [proxy, setProxy] = useState<ProxyStatus | null>(null)
  const [projectID, setProjectID] = useState('')
  const [hostname, setHostname] = useState('')
  const [targetPort, setTargetPort] = useState('')
  const [editingID, setEditingID] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const canMutate = user?.role === 'admin' || user?.role === 'operator'

  async function loadDomains() {
    try {
      setDomains(await request<DomainRecord[]>('/domains'))
      setError('')
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Nie udało się pobrać domen')
    }
  }

  async function loadProxy() {
    try {
      setProxy(await request<ProxyStatus>('/proxy/status'))
    } catch (e: unknown) {
      setProxy({ detected: false, config_valid: false, error: e instanceof Error ? e.message : 'Błąd Nginx' })
    }
  }

  async function refresh() {
    await Promise.all([loadDomains(), loadProxy()])
  }

  useEffect(() => { void refresh() }, [])

  function resetForm() {
    setEditingID(null)
    setProjectID('')
    setHostname('')
    setTargetPort('')
  }

  function edit(domain: DomainRecord) {
    setEditingID(domain.id)
    setProjectID(domain.project_id)
    setHostname(domain.hostname)
    setTargetPort(String(domain.target_port))
    setNotice('')
    setError('')
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    setError('')
    setNotice('')
    try {
      let result: DomainMutationResult
      if (editingID) {
        result = await request<DomainMutationResult>('/domains/' + editingID, {
          method: 'PATCH',
          body: JSON.stringify({ hostname: hostname.trim(), target_port: Number(targetPort) }),
        })
      } else {
        result = await request<DomainMutationResult>('/domains', {
          method: 'POST',
          body: JSON.stringify({ project_id: projectID.trim(), hostname: hostname.trim(), target_port: Number(targetPort) }),
        })
      }
      if (result.hosts.instruction) {
        setNotice(result.hosts.instruction)
      } else {
        setNotice(editingID ? 'Zaktualizowano domenę.' : 'Dodano domenę.')
      }
      resetForm()
      await refresh()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Nie udało się zapisać domeny')
    }
  }

  async function remove(id: string) {
    setError('')
    setNotice('')
    try {
      const result = await request<{ hosts?: { instruction?: string } }>('/domains/' + id, { method: 'DELETE' })
      setNotice(result.hosts?.instruction ?? 'Usunięto domenę.')
      if (editingID === id) resetForm()
      await refresh()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Nie udało się usunąć domeny')
    }
  }

  async function checkHealth(domain: DomainRecord) {
    setError('')
    try {
      await request('/health-checks/run', {
        method: 'POST',
        body: JSON.stringify({
          project_id: domain.project_id,
          type: 'http',
          target: 'http://127.0.0.1:' + domain.target_port + '/',
          timeout_seconds: 3,
        }),
      })
      await loadDomains()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Healthcheck nie powiódł się')
    }
  }

  async function testCandidate() {
    setError('')
    setNotice('')
    try {
      await request('/proxy/test', {
        method: 'POST',
        body: JSON.stringify({ hostname: hostname.trim(), target_port: Number(targetPort) }),
      })
      setNotice('Kandydat konfiguracji Nginx przeszedł walidację.')
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Walidacja Nginx nie powiodła się')
    }
  }

  async function reloadProxy() {
    setError('')
    setNotice('')
    try {
      await request('/proxy/reload', { method: 'POST', body: '{}' })
      setNotice('Nginx przeładowany po poprawnej walidacji konfiguracji.')
      await loadProxy()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Nie udało się przeładować Nginx')
    }
  }

  return <>
    <div className="page-heading">
      <div>
        <h1>Domeny i Proxy</h1>
        <p className="muted">Mapowania hostname → lokalny port aplikacji z kontrolowanym Nginx i healthcheckiem.</p>
      </div>
      <div className="toolbar">
        <span className={'badge ' + (proxy?.detected && proxy.config_valid ? 'badge-ok' : 'badge-warn')}>
          {proxy?.detected ? (proxy.config_valid ? 'Nginx OK' : 'Nginx błąd') : 'Nginx niewykryty'}
        </span>
        <button type="button" className="secondary" onClick={() => void refresh()}>Odśwież</button>
        {user?.role === 'admin' && <button type="button" className="secondary" onClick={() => void reloadProxy()}>Reload Nginx</button>}
      </div>
    </div>

    {proxy?.version && <p className="muted">Wersja: {proxy.version}</p>}
    {proxy?.error && <div className="warning-banner">{proxy.error}</div>}
    {error && <div className="error-banner">{error}</div>}
    {notice && <div className="success-banner">{notice}</div>}

    {canMutate && <form className="panel form-grid" onSubmit={submit}>
      <label>ID projektu
        <input value={projectID} onChange={(e) => setProjectID(e.target.value)} required disabled={editingID !== null} placeholder="project UUID" />
      </label>
      <label>Hostname
        <input value={hostname} onChange={(e) => setHostname(e.target.value)} required placeholder="cloudportal.devbox.local" />
      </label>
      <label>Port aplikacji
        <input type="number" min={1} max={65535} value={targetPort} onChange={(e) => setTargetPort(e.target.value)} required placeholder="8010" />
      </label>
      <div className="form-actions">
        <button type="submit">{editingID ? 'Zapisz zmiany' : 'Dodaj domenę'}</button>
        <button type="button" className="secondary" disabled={!hostname || !targetPort} onClick={() => void testCandidate()}>Testuj config</button>
        {editingID && <button type="button" className="secondary" onClick={resetForm}>Anuluj</button>}
      </div>
    </form>}

    <div className="table-wrap">
      <table>
        <thead><tr><th>Hostname</th><th>Aplikacja</th><th>Target</th><th>Status</th><th>Health</th><th>Port</th><th>HTTPS / certyfikat</th>{canMutate && <th>Akcje</th>}</tr></thead>
        <tbody>
          {domains.map((domain) => <tr key={domain.id}>
            <td><strong>{domain.hostname}</strong></td>
            <td>{domain.application || domain.project_id}</td>
            <td>{domain.target}</td>
            <td><span className="badge badge-ok">{domain.status}</span></td>
            <td>
              {domain.health
                ? <span className={'badge ' + (domain.health.status === 'healthy' ? 'badge-ok' : 'badge-warn')}>
                    {domain.health.status} · {domain.health.response_time_ms} ms
                  </span>
                : <span className="badge badge-muted">brak danych</span>}
            </td>
            <td>{domain.target_port}</td>
            <td><DomainTLS id={domain.id} admin={user?.role === 'admin'} onChanged={() => { void refresh() }} /></td>
            {canMutate && <td><div className="row-actions">
              <button type="button" className="secondary" onClick={() => edit(domain)}>Edytuj</button>
              <button type="button" className="secondary" onClick={() => void checkHealth(domain)}>Health</button>
              <button type="button" className="danger" onClick={() => void remove(domain.id)}>Usuń</button>
            </div></td>}
          </tr>)}
          {domains.length === 0 && <tr><td colSpan={canMutate ? 8 : 7} className="muted">Brak skonfigurowanych domen.</td></tr>}
        </tbody>
      </table>
    </div>
  </>
}

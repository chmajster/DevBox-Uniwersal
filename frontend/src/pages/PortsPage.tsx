import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { request } from '../api/client'
import type { PortRecord } from '../api/types'
import { useAuth } from '../auth/AuthContext'

export function PortsPage() {
  const { user } = useAuth()
  const [ports, setPorts] = useState<PortRecord[]>([])
  const [projectID, setProjectID] = useState('')
  const [purpose, setPurpose] = useState('application')
  const [preferredPort, setPreferredPort] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const canMutate = user?.role === 'admin' || user?.role === 'operator'

  async function load() {
    try {
      setPorts(await request<PortRecord[]>('/ports'))
      setError('')
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Nie udało się pobrać portów')
    }
  }

  useEffect(() => { void load() }, [])

  async function allocate(event: FormEvent) {
    event.preventDefault()
    setError('')
    setNotice('')
    try {
      const payload: { purpose: string; preferred_port?: number } = { purpose }
      if (preferredPort.trim() !== '') payload.preferred_port = Number(preferredPort)
      const item = await request<PortRecord>(
        '/projects/' + encodeURIComponent(projectID.trim()) + '/port/allocate',
        { method: 'POST', body: JSON.stringify(payload) },
      )
      setNotice('Przydzielono port ' + item.port)
      setPreferredPort('')
      await load()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Nie udało się przydzielić portu')
    }
  }

  async function release(port: number) {
    setError('')
    setNotice('')
    try {
      await request<{ port: number; state: string }>('/ports/' + port, { method: 'DELETE' })
      setNotice('Zwolniono port ' + port)
      await load()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Nie udało się zwolnić portu')
    }
  }

  return <>
    <div className="page-heading">
      <div>
        <h1>Porty</h1>
        <p className="muted">Centralny allocator sprawdza rezerwacje SQLite i rzeczywisty socket hosta.</p>
      </div>
      <button type="button" className="secondary" onClick={() => void load()}>Odśwież</button>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {notice && <div className="success-banner">{notice}</div>}

    {canMutate && <form className="panel form-grid" onSubmit={allocate}>
      <label>ID projektu
        <input value={projectID} onChange={(e) => setProjectID(e.target.value)} required placeholder="project UUID" />
      </label>
      <label>Przeznaczenie
        <input value={purpose} onChange={(e) => setPurpose(e.target.value)} required placeholder="application" />
      </label>
      <label>Preferowany port
        <input type="number" min={1} max={65535} value={preferredPort} onChange={(e) => setPreferredPort(e.target.value)} placeholder="automatycznie" />
      </label>
      <div className="form-actions"><button type="submit">Przydziel port</button></div>
    </form>}

    <div className="table-wrap">
      <table>
        <thead><tr><th>Port</th><th>Aplikacja</th><th>Przeznaczenie</th><th>Status</th><th>Socket</th><th>Utworzono</th>{canMutate && <th>Akcje</th>}</tr></thead>
        <tbody>
          {ports.map((port) => <tr key={port.id}>
            <td><strong>{port.port}</strong></td>
            <td>{port.application ?? port.project_id ?? '—'}</td>
            <td>{port.purpose}</td>
            <td><span className={'badge ' + (port.state === 'released' ? 'badge-muted' : 'badge-ok')}>{port.state}</span></td>
            <td>{port.socket_available ? 'wolny' : 'zajęty'}</td>
            <td>{new Date(port.created_at).toLocaleString()}</td>
            {canMutate && <td>{port.state !== 'released' ? <button type="button" className="danger" onClick={() => void release(port.port)}>Zwolnij</button> : '—'}</td>}
          </tr>)}
          {ports.length === 0 && <tr><td colSpan={canMutate ? 7 : 6} className="muted">Brak zapisanych portów.</td></tr>}
        </tbody>
      </table>
    </div>
  </>
}

import { useEffect, useState } from 'react'
import { listHealthChecks, listHealthHistory, runProjectHealthCheck } from '../api/operations'
import type { ApplicationHealth, HealthHistoryEntry } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { ErrorState } from '../components/ErrorState'

export function HealthPage() {
  const { user } = useAuth()
  const [checks, setChecks] = useState<ApplicationHealth[]>([])
  const [history, setHistory] = useState<HealthHistoryEntry[]>([])
  const [projectID, setProjectID] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    setError('')
    void Promise.all([
      listHealthChecks(),
      listHealthHistory(projectID || undefined, 300)
    ]).then(([current, entries]) => {
      if (cancelled) return
      setChecks(current)
      setHistory(entries)
    }).catch((cause: unknown) => {
      if (!cancelled) setError(cause instanceof Error ? cause.message : String(cause))
    })
    return () => { cancelled = true }
  }, [projectID])

  async function run(project: string) {
    setBusy(project)
    setError('')
    try {
      const current = await runProjectHealthCheck(project)
      setChecks(existing => existing.map(item => item.project_id === project ? current : item))
      setHistory(await listHealthHistory(projectID || undefined, 300))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy('')
    }
  }

  const projects = Array.from(new Map(checks.map(item => [item.project_id, item.project_name])).entries())

  return <>
    <div className="page-heading">
      <div>
        <h1>Health</h1>
        <p className="muted">Automatyczne HTTP/TCP health-checki aplikacji oraz historia wyników.</p>
      </div>
    </div>

    {error && <ErrorState message={error} />}

    <div className="panel">
      <h2>Aktualny stan</h2>
      <div className="table-scroll"><table>
        <thead><tr><th>Aplikacja</th><th>Status</th><th>Typ</th><th>Target</th><th>Czas</th><th>Sprawdzono</th><th>Akcja</th></tr></thead>
        <tbody>
          {checks.map(item => <tr key={item.id}>
            <td>{item.project_name}</td>
            <td><strong>{item.status || 'unknown'}</strong>{item.error && <div className="muted">{item.error}</div>}</td>
            <td>{item.type}</td>
            <td><code>{item.target}</code></td>
            <td>{item.response_time_ms} ms</td>
            <td>{item.checked_at ? new Date(item.checked_at).toLocaleString() : '—'}</td>
            <td>{user?.role !== 'viewer' && <button type="button" disabled={busy === item.project_id} onClick={() => void run(item.project_id)}>{busy === item.project_id ? 'Sprawdzanie…' : 'Sprawdź'}</button>}</td>
          </tr>)}
          {checks.length === 0 && <tr><td colSpan={7} className="muted">Brak skonfigurowanych health-checków.</td></tr>}
        </tbody>
      </table></div>
    </div>

    <div className="panel">
      <div className="page-heading">
        <div><h2>Historia</h2></div>
        <label>Aplikacja<select value={projectID} onChange={event => setProjectID(event.target.value)}>
          <option value="">Wszystkie</option>
          {projects.map(([id, name]) => <option key={id} value={id}>{name}</option>)}
        </select></label>
      </div>
      <div className="table-scroll"><table>
        <thead><tr><th>Czas</th><th>Aplikacja</th><th>Status</th><th>Response</th><th>Komunikat</th></tr></thead>
        <tbody>
          {history.map(item => <tr key={item.id}>
            <td>{new Date(item.checked_at).toLocaleString()}</td>
            <td>{item.project_name}</td>
            <td>{item.status}</td>
            <td>{item.response_time_ms} ms</td>
            <td>{item.error || item.message || '—'}</td>
          </tr>)}
          {history.length === 0 && <tr><td colSpan={5} className="muted">Brak historii.</td></tr>}
        </tbody>
      </table></div>
    </div>
  </>
}

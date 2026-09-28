import { useCallback, useEffect, useMemo, useState } from 'react'
import { request } from '../api/client'
import type { Job, ManagedRuntimeType, RuntimeInstallation, RuntimeTypeView } from '../api/types'
import { useAuth } from '../auth/AuthContext'

const runtimeTypes: ManagedRuntimeType[] = ['php', 'node', 'python', 'go']

type ActiveJob = { runtimeType: ManagedRuntimeType; job: Job; stage?: string }

export function RuntimeManagerPage() {
  const { user } = useAuth()
  const [views, setViews] = useState<RuntimeTypeView[]>([])
  const [selected, setSelected] = useState<Record<string, string>>({})
  const [query, setQuery] = useState('')
  const [activeJob, setActiveJob] = useState<ActiveJob | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    const data = await Promise.all(runtimeTypes.map((runtimeType) =>
      request<RuntimeTypeView>(`/runtimes/${runtimeType}?available=true`)
    ))
    setViews(data)
    setSelected((current) => {
      const next = { ...current }
      for (const view of data) {
        if (!next[view.runtime_type]) next[view.runtime_type] = view.available?.[0]?.version ?? ''
      }
      return next
    })
  }, [])

  useEffect(() => {
    load().catch((reason: unknown) => setError(reason instanceof Error ? reason.message : 'Nie udało się pobrać runtime'))
  }, [load])

  useEffect(() => {
    if (!activeJob || ['completed', 'failed', 'cancelled'].includes(activeJob.job.status)) return
    const timer = window.setInterval(() => {
      request<Job>(`/jobs/${encodeURIComponent(activeJob.job.id)}`)
        .then((job) => {
          setActiveJob((current) => current ? { ...current, job } : null)
          if (['completed', 'failed', 'cancelled'].includes(job.status)) {
            void load()
          }
        })
        .catch(() => undefined)
    }, 1500)
    return () => window.clearInterval(timer)
  }, [activeJob, load])

  const visibleViews = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return views
    return views.filter((view) =>
      view.runtime_type.includes(needle) ||
      view.installations.some((item) => item.version.toLowerCase().includes(needle) || item.executable_path.toLowerCase().includes(needle)) ||
      (view.available ?? []).some((item) => item.version.toLowerCase().includes(needle))
    )
  }, [query, views])

  async function install(runtimeType: ManagedRuntimeType) {
    const version = selected[runtimeType]?.trim()
    if (!version) return
    setBusy(true)
    setError('')
    try {
      const result = await request<{ installation: RuntimeInstallation; job: Job }>(`/runtimes/${runtimeType}/install`, {
        method: 'POST',
        body: JSON.stringify({ version })
      })
      setActiveJob({ runtimeType, job: result.job })
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Instalacja runtime nie powiodła się')
    } finally {
      setBusy(false)
    }
  }

  async function mutate(path: string, init: RequestInit) {
    setBusy(true)
    setError('')
    try {
      await request<unknown>(path, init)
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Operacja runtime nie powiodła się')
    } finally {
      setBusy(false)
    }
  }

  function remove(runtimeType: ManagedRuntimeType, item: RuntimeInstallation) {
    if (!window.confirm(`Usunąć ${runtimeType} ${item.version}? Runtime używany przez projekt zostanie zablokowany po stronie API.`)) return
    void mutate(`/runtimes/${runtimeType}/installations/${encodeURIComponent(item.id)}`, { method: 'DELETE' })
  }

  return <>
    <div className="page-heading">
      <div>
        <h1>Runtimes</h1>
        <p className="muted">Wiele wersji PHP, Node.js, Python i Go bez zmiany globalnego PATH. Systemowe runtime są wykrywane, ale DevBox ich nie usuwa.</p>
      </div>
      <input className="runtime-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Szukaj wersji lub ścieżki…" aria-label="Szukaj runtime" />
    </div>

    {error && <div className="error-banner">{error}</div>}
    {activeJob && <div className="panel runtime-job">
      <strong>Job: {activeJob.runtimeType} · {activeJob.job.status}</strong>
      <span className="muted">{activeJob.job.error || activeJob.stage || 'Etapy instalacji są zapisywane w centralnych logach zadania.'}</span>
      <a href={`/jobs?job=${encodeURIComponent(activeJob.job.id)}`}>Otwórz log zadania</a>
    </div>}

    <div className="runtime-version-grid">
      {visibleViews.map((view) => <section className="panel runtime-version-card" key={view.runtime_type}>
        <div className="runtime-version-heading">
          <div>
            <h2>{view.runtime_type === 'node' ? 'Node.js' : view.runtime_type.toUpperCase()}</h2>
            <p className="muted">Domyślna: {view.default?.resolved_version ?? 'nie ustawiono'}</p>
          </div>
          <span className="badge badge-muted">{view.installations.length} wykrytych</span>
        </div>

        {user?.role === 'admin' && <div className="runtime-install-row">
          <label>Wersja do instalacji
            <input
              list={`available-${view.runtime_type}`}
              value={selected[view.runtime_type] ?? ''}
              onChange={(event) => setSelected((current) => ({ ...current, [view.runtime_type]: event.target.value }))}
              placeholder="np. 22 lub 22.15.0"
            />
          </label>
          <datalist id={`available-${view.runtime_type}`}>
            {(view.available ?? []).slice(0, 200).map((item) => <option key={item.version} value={item.version}>{item.source}</option>)}
          </datalist>
          <button type="button" disabled={busy || !selected[view.runtime_type]} onClick={() => void install(view.runtime_type)}>Install</button>
        </div>}

        <div className="table-scroll">
          <table className="runtime-version-table">
            <thead><tr><th>Wersja</th><th>Źródło</th><th>Status</th><th>Arch.</th><th>Executable</th><th>Projekty</th><th>Akcje</th></tr></thead>
            <tbody>
              {view.installations.map((item) => <tr key={item.id}>
                <td><strong>{item.version}</strong>{view.default?.runtime_installation_id === item.id && <div className="small muted">default</div>}</td>
                <td>{item.managed_by_devbox ? 'DevBox Managed' : 'System'}<div className="small muted">{item.installation_method}</div></td>
                <td><span className={`badge ${item.status === 'installed' ? 'badge-ok' : item.status === 'failed' || item.status === 'broken' ? 'badge-warn' : 'badge-muted'}`}>{item.status}</span>{item.error && <div className="small error-cell">{item.error}</div>}</td>
                <td>{item.platform}/{item.architecture}</td>
                <td><code className="mono">{item.executable_path || '—'}</code></td>
                <td>{item.used_by_projects?.length ?? 0}{(item.used_by_projects?.length ?? 0) > 0 && <div className="small muted">{item.used_by_projects?.map((project) => project.name).join(', ')}</div>}</td>
                <td className="actions">
                  {user?.role === 'admin' && <>
                    <button type="button" className="secondary" disabled={busy} onClick={() => void mutate(`/runtimes/installations/${encodeURIComponent(item.id)}/validate`, { method: 'POST', body: '{}' })}>Validate</button>
                    {item.status === 'installed' && <button type="button" className="secondary" disabled={busy} onClick={() => void mutate(`/runtime-defaults/${view.runtime_type}`, { method: 'PUT', body: JSON.stringify({ runtime_installation_id: item.id }) })}>Set default</button>}
                    {item.managed_by_devbox && <button type="button" className="danger" disabled={busy || (item.used_by_projects?.length ?? 0) > 0} onClick={() => remove(view.runtime_type, item)}>Remove</button>}
                  </>}
                </td>
              </tr>)}
              {view.installations.length === 0 && <tr><td colSpan={7} className="muted">Brak wykrytych instalacji.</td></tr>}
            </tbody>
          </table>
        </div>
      </section>)}
    </div>
  </>
}

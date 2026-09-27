import { useEffect, useState } from 'react'
import { request } from '../api/client'
import type { RuntimeInfo } from '../api/types'

export function RuntimeManagerPage() {
  const [runtimes, setRuntimes] = useState<RuntimeInfo[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    request<RuntimeInfo[]>('/runtimes')
      .then(setRuntimes)
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : 'Failed to load runtimes'))
  }, [])

  return <>
    <h1>Runtime Manager</h1>
    <p className="muted">Host availability for Static, PHP, Python, Go and Node.js runtime providers.</p>
    {error && <div className="error-banner">{error}</div>}
    <div className="runtime-grid">
      {runtimes.map((runtime) => <article className="runtime-card" key={runtime.runtime}>
        <div className="runtime-card-header">
          <strong>{runtime.runtime}</strong>
          <span className={`status-pill status-${runtime.status}`}>{runtime.status}</span>
        </div>
        <div className="runtime-version">{runtime.version ?? 'Version unavailable'}</div>
        <div className="runtime-dependencies">
          {(runtime.dependencies ?? []).map((dependency) => <div key={dependency.name}>
            <span>{dependency.name}</span>
            <span className={`status-text status-${dependency.status}`}>{dependency.status}</span>
            <code>{dependency.version ?? '—'}</code>
          </div>)}
        </div>
      </article>)}
    </div>
  </>
}

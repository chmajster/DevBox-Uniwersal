import { useEffect, useState } from 'react'
import { request } from '../api/client'
import type { RuntimeChoice } from '../applications/components'

export function RuntimeManagerPage() {
  const [runtimes, setRuntimes] = useState<RuntimeChoice[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    request<RuntimeChoice[]>('/runtimes/catalog')
      .then(setRuntimes)
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : 'Failed to load runtimes'))
  }, [])

  return <>
    <h1>Runtime Manager</h1>
    <p className="muted">Runtime aplikacji działa w kontenerze Docker. Wersję wybierasz osobno dla każdej aplikacji.</p>
    {error && <div className="error-banner">{error}</div>}
    <div className="runtime-grid">
      {runtimes.map((runtime) => <article className="runtime-card" key={runtime.name}>
        <div className="runtime-card-header">
          <strong>{runtime.name}</strong>
          <span className={`status-pill status-${'available'}`}>{'available'}</span>
        </div>
        <div className="runtime-version">{runtime.versions.join(' · ')}</div>
        <p>Domyślnie: {runtime.default_version}</p>
      </article>)}
    </div>
  </>
}

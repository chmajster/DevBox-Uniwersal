import { useEffect, useState } from 'react'
import { request } from '../api/client'
import type { ProjectRuntimeInfo, RuntimeValidation } from '../api/types'
import { useAuth } from '../auth/AuthContext'

interface Props {
  projectId: string
}

export function ProjectRuntimeSection({ projectId }: Props) {
  const { user } = useAuth()
  const [runtime, setRuntime] = useState<ProjectRuntimeInfo | null>(null)
  const [validation, setValidation] = useState<RuntimeValidation | null>(null)
  const [error, setError] = useState('')
  const [validating, setValidating] = useState(false)

  useEffect(() => {
    setError('')
    setValidation(null)
    request<ProjectRuntimeInfo>(`/projects/${encodeURIComponent(projectId)}/runtime`)
      .then(setRuntime)
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : 'Failed to load project runtime'))
  }, [projectId])

  async function validate() {
    setValidating(true)
    setError('')
    try {
      const result = await request<RuntimeValidation>(`/projects/${encodeURIComponent(projectId)}/runtime/validate`, {
        method: 'POST',
        body: '{}'
      })
      setValidation(result)
    } catch (reason: unknown) {
      setError(reason instanceof Error ? reason.message : 'Runtime validation failed')
    } finally {
      setValidating(false)
    }
  }

  return <section className="runtime-section">
    <div className="section-heading">
      <div>
        <h2>Runtime</h2>
        <p className="muted">Detection, host availability and project-specific runtime commands.</p>
      </div>
      {user?.role !== 'viewer' && <button type="button" onClick={validate} disabled={validating}>
        {validating ? 'Validating…' : 'Validate runtime'}
      </button>}
    </div>

    {error && <div className="error-banner">{error}</div>}
    {runtime && <>
      <div className="cards runtime-summary">
        <article><span>Detected runtime</span><strong>{runtime.runtime}</strong></article>
        <article><span>Framework</span><strong>{runtime.framework || 'Generic'}</strong></article>
        <article><span>Availability</span><strong className={`status-text status-${runtime.availability}`}>{runtime.availability}</strong></article>
        <article><span>Version</span><strong>{runtime.version ?? 'Unknown'}</strong></article>
        <article><span>Confidence</span><strong>{runtime.confidence}%</strong></article>
        <article><span>Configured runtime</span><strong>{runtime.configured_runtime || 'Auto-detect'}</strong></article>
      </div>

      <div className="runtime-detail-grid">
        <article>
          <h3>Commands</h3>
          <label>Build</label>
          <code className="command-block">{runtime.build_command || 'No build command required'}</code>
          <label>Start</label>
          <code className="command-block">{runtime.start_command || 'No start command detected'}</code>
        </article>
        <article>
          <h3>Detected files</h3>
          <ul>{runtime.detected_files.map((file) => <li key={file}><code>{file}</code></li>)}</ul>
        </article>
        <article>
          <h3>Dependencies</h3>
          <ul>{(runtime.dependencies ?? []).map((dependency) => <li key={dependency.name}>
            <strong>{dependency.name}</strong> · <span className={`status-text status-${dependency.status}`}>{dependency.status}</span>
            {dependency.version && <> · <code>{dependency.version}</code></>}
          </li>)}</ul>
        </article>
        <article>
          <h3>Environment</h3>
          {runtime.environment && Object.keys(runtime.environment).length > 0
            ? <ul>{Object.entries(runtime.environment).map(([key, value]) => <li key={key}><code>{key}</code> = <code>{value}</code></li>)}</ul>
            : <p className="muted">No runtime environment variables configured.</p>}
        </article>
      </div>
    </>}

    {validation && <div className={validation.valid ? 'validation-box validation-ok' : 'validation-box validation-fail'}>
      <strong>{validation.valid ? 'Runtime configuration is valid' : 'Runtime configuration is invalid'}</strong>
      {(validation.errors ?? []).map((message) => <div key={message}>{message}</div>)}
      {(validation.warnings ?? []).map((message) => <div key={message}>Warning: {message}</div>)}
    </div>}
  </section>
}

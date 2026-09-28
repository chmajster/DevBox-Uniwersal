import { useCallback, useEffect, useState } from 'react'
import { request } from '../api/client'
import type { ManagedRuntimeType, ProjectRuntimeInfo, ProjectRuntimeVersionView, RuntimeValidation } from '../api/types'
import { useAuth } from '../auth/AuthContext'

interface Props {
  projectId: string
}

export function ProjectRuntimeSection({ projectId }: Props) {
  const { user } = useAuth()
  const [runtime, setRuntime] = useState<ProjectRuntimeInfo | null>(null)
  const [versions, setVersions] = useState<ProjectRuntimeVersionView[]>([])
  const [validation, setValidation] = useState<RuntimeValidation | null>(null)
  const [error, setError] = useState('')
  const [busyType, setBusyType] = useState('')
  const [validating, setValidating] = useState(false)

  const load = useCallback(async () => {
    const [detected, assignments] = await Promise.all([
      request<ProjectRuntimeInfo>(`/projects/${encodeURIComponent(projectId)}/runtime`),
      request<ProjectRuntimeVersionView[]>(`/projects/${encodeURIComponent(projectId)}/runtimes`)
    ])
    setRuntime(detected)
    setVersions(assignments)
  }, [projectId])

  useEffect(() => {
    setError('')
    setValidation(null)
    load().catch((reason: unknown) => setError(reason instanceof Error ? reason.message : 'Failed to load project runtime'))
  }, [load])

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

  async function assign(runtimeType: ManagedRuntimeType, installationID: string) {
    setBusyType(runtimeType)
    setError('')
    try {
      await request<unknown>(`/projects/${encodeURIComponent(projectId)}/runtimes/${runtimeType}`, {
        method: 'PUT',
        body: JSON.stringify({ runtime_installation_id: installationID })
      })
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Nie udało się zmienić wersji runtime')
    } finally {
      setBusyType('')
    }
  }

  async function recreatePythonVenv() {
    if (!window.confirm('Usunąć wyłącznie katalog .venv tego projektu? Zostanie odtworzony przy kolejnej instalacji zależności przy użyciu wybranego Pythona.')) return
    setBusyType('python')
    setError('')
    try {
      await request<unknown>(`/projects/${encodeURIComponent(projectId)}/runtimes/python/venv/recreate`, {
        method: 'POST',
        body: JSON.stringify({ confirm: true })
      })
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Nie udało się przebudować virtualenv')
    } finally {
      setBusyType('')
    }
  }

  return <section className="runtime-section">
    <div className="section-heading">
      <div>
        <h2>Runtime</h2>
        <p className="muted">Jawne przypisania executable per projekt. Brak przypisania zachowuje kompatybilny fallback do dotychczasowego host discovery.</p>
      </div>
      {user?.role !== 'viewer' && <button type="button" onClick={() => void validate()} disabled={validating}>
        {validating ? 'Validating…' : 'Validate runtime'}
      </button>}
    </div>

    {error && <div className="error-banner">{error}</div>}

    <div className="runtime-assignment-grid">
      {versions.map((item) => {
        const currentID = item.assignment?.runtime_installation_id ?? ''
        return <article className="panel runtime-assignment-card" key={item.runtime_type}>
          <div className="runtime-version-heading">
            <div>
              <h3>{item.runtime_type === 'node' ? 'Node.js' : item.runtime_type.toUpperCase()}</h3>
              <p className="muted">Wymaganie projektu: {item.requirement || 'nie wykryto'}</p>
            </div>
            {item.assignment && <span className="badge badge-ok">{item.assignment.resolved_version}</span>}
          </div>

          {item.requirement_satisfied === false && <div className="warning-banner">
            Required {item.runtime_type} {item.requirement}, selected {item.assignment?.resolved_version ?? 'none'}.
            <div><a href="/runtimes">Install compatible runtime</a></div>
          </div>}

          <label>Version
            <select
              value={currentID}
              disabled={user?.role === 'viewer' || busyType === item.runtime_type}
              onChange={(event) => void assign(item.runtime_type, event.target.value)}
            >
              <option value="">Auto-detect / not explicitly assigned</option>
              {item.compatible_installed.map((installation) => <option key={installation.id} value={installation.id}>
                {installation.version} · {installation.managed_by_devbox ? 'DevBox Managed' : 'System'} · {installation.architecture}
              </option>)}
              {item.assignment && !item.compatible_installed.some((installation) => installation.id === item.assignment?.runtime_installation_id) &&
                <option value={item.assignment.runtime_installation_id}>{item.assignment.resolved_version} · incompatible with detected requirement</option>}
            </select>
          </label>

          {item.assignment && <div className="runtime-assignment-details">
            <code className="mono">{item.assignment.executable_path}</code>
            <span className="muted">{item.assignment.managed_by_devbox ? 'Managed by DevBox' : 'System runtime'} · {item.assignment.status}</span>
          </div>}

          {item.runtime_type === 'python' && user?.role !== 'viewer' && item.assignment &&
            <button type="button" className="secondary" disabled={busyType === 'python'} onClick={() => void recreatePythonVenv()}>Recreate .venv</button>}
        </article>
      })}
    </div>

    {runtime && <>
      <div className="cards runtime-summary">
        <article><span>Detected runtime</span><strong>{runtime.runtime}</strong></article>
        <article><span>Framework</span><strong>{runtime.framework || 'Generic'}</strong></article>
        <article><span>Availability</span><strong className={`status-text status-${runtime.availability}`}>{runtime.availability}</strong></article>
        <article><span>Detected version</span><strong>{runtime.version ?? 'Unknown'}</strong></article>
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

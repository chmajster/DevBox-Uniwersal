import { useEffect, useMemo, useState } from 'react'
import { ProjectPortsSection } from './ProjectPortsSection'
import { request } from '../api/client'
import type { Job, ProjectRuntimeInfo, RuntimeContainerConfig, RuntimeModuleOption } from '../api/types'
import { useAuth } from '../auth/AuthContext'

interface Props {
  projectId: string
}

const runtimeOptions = [
  { value: '', label: 'Automatycznie wykryj' },
  { value: 'php', label: 'PHP' },
  { value: 'node', label: 'Node.js' },
  { value: 'python', label: 'Python' },
  { value: 'go', label: 'Go' },
  { value: 'static', label: 'Static / HTML' },
] as const

const emptyConfig: RuntimeContainerConfig = {
  project_id: '',
  runtime: '',
  runtime_version: '',
  container_policy: 'auto',
  modules: [],
}

export function ProjectRuntimeSection({ projectId }: Props) {
  const { user } = useAuth()
  const [runtime, setRuntime] = useState<ProjectRuntimeInfo | null>(null)
  const [config, setConfig] = useState<RuntimeContainerConfig>({ ...emptyConfig, project_id: projectId })
  const [catalog, setCatalog] = useState<RuntimeModuleOption[]>([])
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')

  useEffect(() => {
    setError('')
    setMessage('')
    Promise.all([
      request<ProjectRuntimeInfo>(`/projects/${encodeURIComponent(projectId)}/runtime`).catch(() => null),
      request<RuntimeContainerConfig>(`/projects/${encodeURIComponent(projectId)}/runtime/config`),
    ])
      .then(([runtimeInfo, runtimeConfig]) => {
        setRuntime(runtimeInfo)
        setConfig(runtimeConfig)
      })
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : 'Nie udało się wczytać konfiguracji runtime'))
  }, [projectId])

  useEffect(() => {
    if (config.runtime !== 'php') {
      setCatalog([])
      setQuery('')
      return
    }
    request<RuntimeModuleOption[]>('/runtimes/php/modules')
      .then((items) => setCatalog(items ?? []))
      .catch((reason: unknown) => {
        setCatalog([])
        setError(reason instanceof Error ? reason.message : 'Nie udało się wczytać listy modułów PHP')
      })
  }, [config.runtime])

  const selected = useMemo(() => new Set(config.modules.map((item) => item.name)), [config.modules])
  const filteredCatalog = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return catalog
    return catalog.filter((item) =>
      item.name.toLowerCase().includes(needle) ||
      item.label.toLowerCase().includes(needle) ||
      item.description.toLowerCase().includes(needle)
    )
  }, [catalog, query])

  function toggleModule(name: string, enabled: boolean) {
    setConfig((current) => ({
      ...current,
      modules: enabled
        ? [...current.modules.filter((item) => item.name !== name), { name }]
        : current.modules.filter((item) => item.name !== name),
    }))
  }

  async function saveAndRebuild() {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const saved = await request<RuntimeContainerConfig>(`/projects/${encodeURIComponent(projectId)}/runtime/config`, {
        method: 'PUT',
        body: JSON.stringify(config),
      })
      setConfig(saved)
      const job = await request<Job>(`/projects/${encodeURIComponent(projectId)}/runtime/rebuild`, {
        method: 'POST',
        body: '{}',
      })
      setMessage(`Konfiguracja zapisana. Przebudowa kontenera została dodana do kolejki: ${job.id.slice(0, 12)}.`)
    } catch (reason: unknown) {
      setError(reason instanceof Error ? reason.message : 'Nie udało się zapisać konfiguracji runtime')
    } finally {
      setBusy(false)
    }
  }

  const readOnly = user?.role === 'viewer'

  return <><section className="runtime-section panel">
    <div className="section-heading">
      <div>
        <h2>Runtime i kontener</h2>
        <p className="muted">Runtime aplikacji działa wyłącznie w Dockerze. DevBox nie instaluje PHP, Go, Node.js ani Pythona na hoście.</p>
      </div>
      {!readOnly && <button type="button" onClick={() => void saveAndRebuild()} disabled={busy}>
        {busy ? 'Zapisywanie…' : 'Zapisz i przebuduj kontener'}
      </button>}
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="validation-box validation-ok">{message}</div>}

    <div className="form-grid">
      <label>Runtime
        <select
          disabled={readOnly}
          value={config.runtime}
          onChange={(event) => setConfig({ ...config, runtime: event.target.value, runtime_version: '', modules: [] })}
        >
          {runtimeOptions.map((item) => <option key={item.value || 'auto'} value={item.value}>{item.label}</option>)}
        </select>
      </label>

      <label>Wersja obrazu runtime
        <input
          disabled={readOnly || !config.runtime}
          value={config.runtime_version}
          onChange={(event) => setConfig({ ...config, runtime_version: event.target.value })}
          placeholder="puste = domyślna"
        />
      </label>

      <label className="span-2">Polityka kontenera
        <select
          disabled={readOnly}
          value={config.container_policy}
          onChange={(event) => setConfig({ ...config, container_policy: event.target.value as 'auto' | 'custom' })}
        >
          <option value="auto">Automatyczna — użyj Compose/Dockerfile projektu, a jeśli ich nie ma wygeneruj obraz DevBox</option>
          <option value="custom">Własny Docker — wymagany Compose lub Dockerfile projektu</option>
        </select>
      </label>
    </div>

    {runtime && <dl className="runtime-summary" aria-label="Podsumowanie wykrytego runtime">
      <div><dt>Wykryty runtime</dt><dd>{runtime.runtime}</dd></div>
      <div><dt>Framework</dt><dd>{runtime.framework || 'Generic'}</dd></div>
      <div><dt>Pewność detekcji</dt><dd>{runtime.confidence}%</dd></div>
      <div><dt>Aktywny kontener</dt><dd>{config.container_name || 'jeszcze nie utworzony'}</dd></div>
      <div><dt>Obraz</dt><dd><code>{config.image_tag || '—'}</code></dd></div>
      <div><dt>Fingerprint</dt><dd><code>{config.build_fingerprint?.slice(0, 16) || '—'}</code></dd></div>
    </dl>}

    {config.runtime === 'php' && <div className="runtime-modules">
      <div className="section-heading">
        <div>
          <h3>Moduły PHP w kontenerze</h3>
          <p className="muted">Wybierz rozszerzenia wymagane przez tę aplikację. DevBox instaluje je podczas budowania obrazu PHP wewnątrz kontenera; PHP na hoście nie jest modyfikowane.</p>
          <p className="muted small">Wybrane moduły: <strong>{selected.size}</strong>. Zmiana listy wymaga przebudowania kontenera.</p>
        </div>
        <input
          aria-label="Szukaj modułów PHP"
          placeholder="Szukaj modułu PHP…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>
      <div className="runtime-module-list">
        {filteredCatalog.map((item) => <label className="runtime-module-row" key={item.name}>
          <input
            type="checkbox"
            disabled={readOnly}
            checked={selected.has(item.name)}
            onChange={(event) => toggleModule(item.name, event.target.checked)}
          />
          <span><strong>{item.label}</strong><code>{item.name}</code><small>{item.description}</small></span>
        </label>)}
        {filteredCatalog.length === 0 && <p className="muted">Brak modułów PHP pasujących do wyszukiwania.</p>}
      </div>
    </div>}
  </section><ProjectPortsSection key={projectId} projectId={projectId} /></>
}

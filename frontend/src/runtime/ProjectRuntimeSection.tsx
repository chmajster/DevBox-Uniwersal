import { useEffect, useMemo, useState } from 'react'
import { ProjectPortsSection } from './ProjectPortsSection'
import { request } from '../api/client'
import type { Job, ProjectRuntimeInfo, RuntimeContainerConfig, RuntimeModuleOption } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { effectiveRuntimeName, preparePHPModuleConfig, updatePHPModuleSelection } from './phpModuleConfig'
import { PHPModulePicker } from './PHPModulePicker'

interface Props {
  projectId: string
  showPorts?: boolean
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

export function ProjectRuntimeSection({ projectId, showPorts = true }: Props) {
  const { user } = useAuth()
  const [runtime, setRuntime] = useState<ProjectRuntimeInfo | null>(null)
  const [config, setConfig] = useState<RuntimeContainerConfig>({ ...emptyConfig, project_id: projectId })
  const [catalog, setCatalog] = useState<RuntimeModuleOption[]>([])
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

  const effectiveRuntime = effectiveRuntimeName(config.runtime, runtime?.runtime)

  useEffect(() => {
    if (effectiveRuntime !== 'php') {
      setCatalog([])
      return
    }
    request<RuntimeModuleOption[]>('/runtimes/php/modules')
      .then((items) => setCatalog(items ?? []))
      .catch((reason: unknown) => {
        setCatalog([])
        setError(reason instanceof Error ? reason.message : 'Nie udało się wczytać listy modułów PHP')
      })
  }, [effectiveRuntime])

  const selected = useMemo(() => new Set(config.modules.map((item) => item.name)), [config.modules])
  function toggleModule(name: string, enabled: boolean) {
    setConfig((current) => ({
      ...preparePHPModuleConfig(current),
      modules: updatePHPModuleSelection(current.modules, name, enabled),
    }))
  }

  function replaceModules(names: string[]) {
    setConfig((current) => ({
      ...preparePHPModuleConfig(current),
      modules: names.map((name) => ({ name })),
    }))
  }

  async function saveAndRebuild() {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const payload = effectiveRuntime === 'php' && config.modules.length > 0 ? preparePHPModuleConfig(config) : config
      const saved = await request<RuntimeContainerConfig>(`/projects/${encodeURIComponent(projectId)}/runtime/config`, {
        method: 'PUT',
        body: JSON.stringify(payload),
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

    {effectiveRuntime === 'php' && <div className="runtime-modules">
      <div className="runtime-modules-heading">
        <div>
          <h3>Moduły PHP</h3>
          <p className="muted">Wybierz rozszerzenia dostępne wewnątrz kontenera aplikacji. Kliknięcie całego wiersza zaznacza moduł.</p>
        </div>
        <span className="runtime-module-count">{selected.size} wybranych</span>
      </div>
      <PHPModulePicker
        catalog={catalog}
        selected={selected}
        disabled={readOnly || busy}
        onToggle={toggleModule}
        onSelectionChange={replaceModules}
      />
      <p className="muted small runtime-modules-note">PHP na hoście pozostaje bez zmian. DevBox modyfikuje wyłącznie generowany obraz kontenera.</p>
    </div>}
  </section>{showPorts && <ProjectPortsSection key={projectId} projectId={projectId} />}</>
}

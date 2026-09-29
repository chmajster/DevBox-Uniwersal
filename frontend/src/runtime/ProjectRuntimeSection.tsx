import { useEffect, useMemo, useState } from 'react'
import { ProjectPortsSection } from './ProjectPortsSection'
import { request } from '../api/client'
import type { Job, ProjectRuntimeInfo, RuntimeContainerConfig, RuntimeModuleOption, RuntimeExecutionOptions } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { effectiveRuntimeName, preparePHPModuleConfig, updatePHPModuleSelection } from './phpModuleConfig'

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
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [writableText, setWritableText] = useState('')
  const [outputsText, setOutputsText] = useState('')
  const [statusesText, setStatusesText] = useState('')

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
        setWritableText((runtimeConfig.execution?.writable_paths ?? []).join(', '))
        setOutputsText((runtimeConfig.execution?.build_outputs ?? []).join(', '))
        setStatusesText((runtimeConfig.execution?.healthcheck?.expected_statuses ?? []).join(', '))
      })
      .catch((reason: unknown) => setError(reason instanceof Error ? reason.message : 'Nie udało się wczytać konfiguracji runtime'))
  }, [projectId])

  const effectiveRuntime = effectiveRuntimeName(config.runtime, runtime?.runtime)

  useEffect(() => {
    if (effectiveRuntime !== 'php') {
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
  }, [effectiveRuntime])

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
      ...preparePHPModuleConfig(current),
      modules: updatePHPModuleSelection(current.modules, name, enabled),
    }))
  }

  async function saveAndRebuild() {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const parseList = (value: string) => value.split(',').map((entry) => entry.trim()).filter(Boolean)
      const statuses = parseList(statusesText).map(Number)
      if (statuses.some((status) => !Number.isInteger(status) || status < 100 || status > 599)) throw new Error('Kody HTTP muszą być liczbami całkowitymi od 100 do 599.')
      const configured: RuntimeContainerConfig = { ...config, execution: { ...config.execution, writable_paths: parseList(writableText), build_outputs: outputsText.trim() ? parseList(outputsText) : undefined, healthcheck: { ...config.execution?.healthcheck, expected_statuses: statuses } } }
      const payload = effectiveRuntime === 'php' && configured.modules.length > 0 ? preparePHPModuleConfig(configured) : configured
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

  function updateExecution(patch: Partial<RuntimeExecutionOptions>) {
    setConfig((current) => ({ ...current, execution: { ...current.execution, ...patch } }))
  }
  const execution = config.execution ?? {}
  const health = execution.healthcheck ?? {}
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

    <fieldset disabled={readOnly || busy || config.container_policy === 'custom'}>
      <legend>Wykonywanie generowanego kontenera</legend>
      <div className="form-grid">
        <label>Źródła aplikacji<select value={execution.source_mode || 'live'} onChange={(e) => updateExecution({ source_mode: e.target.value as 'live' | 'versioned' })}><option value="live">Live Code — katalog hosta</option><option value="versioned">Wersjonowane — kod w obrazie</option></select></label>
        <p className="muted">Live Code pokazuje edycje od razu, lecz rollback kontenera nie cofa plików hosta. Tryb wersjonowany zachowuje kod w obrazie; nie cofa zmian w bazie ani danych aplikacji.</p>
        <label>UID (0 = domyślny obrazu)<input type="number" min={0} max={2147483647} value={execution.uid ?? 0} onChange={(e) => updateExecution({ uid: Number(e.target.value) })} /></label>
        <label>GID (0 = domyślny obrazu)<input type="number" min={0} max={2147483647} value={execution.gid ?? 0} onChange={(e) => updateExecution({ gid: Number(e.target.value) })} /></label>
        <label>Katalogi wymagające zapisu<input value={writableText} onChange={(e) => setWritableText(e.target.value)} placeholder="storage, bootstrap/cache, uploads" /></label>
        <label>Wyniki budowania chronione przed bind mountem<input value={outputsText} onChange={(e) => setOutputsText(e.target.value)} placeholder="Node: domyślnie dist, build, .next, .nuxt, .output, out" /></label>
        {effectiveRuntime === 'go' && <label className="checkbox"><input type="checkbox" checked={execution.cgo ?? false} onChange={(e) => updateExecution({ cgo: e.target.checked })} /> Włącz CGO (obraz wykonawczy Debian)</label>}
      </div>
      <p className="muted small">UID i GID należy ustawić razem. DevBox sprawdza uprawnienia z wnętrza kontenera; nie zmienia właściciela ani praw całego katalogu hosta.</p>
    </fieldset>
    <fieldset disabled={readOnly || busy}>
      <legend>Gotowość aplikacji</legend>
      <div className="form-grid">
        <label>Ścieżka HTTP lub TCP<input value={health.target ?? ''} onChange={(e) => updateExecution({ healthcheck: { ...health, target: e.target.value } })} placeholder="/health lub tcp; puste = ustawienie projektu" /></label>
        <label>Czas rozruchu (sekundy)<input type="number" min={1} max={600} value={health.startup_seconds || 60} onChange={(e) => updateExecution({ healthcheck: { ...health, startup_seconds: Number(e.target.value) } })} /></label>
        <label>Timeout pojedynczej próby (sekundy)<input type="number" min={1} max={30} value={health.timeout_seconds || 2} onChange={(e) => updateExecution({ healthcheck: { ...health, timeout_seconds: Number(e.target.value) } })} /></label>
        <label>Oczekiwane kody HTTP<input value={statusesText} onChange={(e) => setStatusesText(e.target.value)} placeholder="Domyślnie 200–399; np. 200, 204" /></label>
      </div>
    </fieldset>

    {runtime && <dl className="runtime-summary" aria-label="Podsumowanie wykrytego runtime">
      <div><dt>Wykryty runtime</dt><dd>{runtime.runtime}</dd></div>
      <div><dt>Framework</dt><dd>{runtime.framework || 'Generic'}</dd></div>
      <div><dt>Pewność detekcji</dt><dd>{runtime.confidence}%</dd></div>
      <div><dt>Aktywny kontener</dt><dd>{config.container_name || 'jeszcze nie utworzony'}</dd></div>
      <div><dt>Obraz</dt><dd><code>{config.image_tag || '—'}</code></dd></div>
      <div><dt>Fingerprint</dt><dd><code>{config.build_fingerprint?.slice(0, 16) || '—'}</code></dd></div>
    </dl>}

    {effectiveRuntime === 'php' && <div className="runtime-modules">
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
  </section>{showPorts && <ProjectPortsSection key={projectId} projectId={projectId} />}</>
}

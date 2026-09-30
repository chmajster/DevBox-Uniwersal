import { useEffect, useMemo, useState } from 'react'
import { request } from '../api/client'
import type { Job, ProjectRuntimeInfo, RuntimeContainerConfig, RuntimeModuleOption } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { effectiveRuntimeName, preparePHPModuleConfig, updatePHPModuleSelection } from './phpModuleConfig'
import { PHPModulePicker } from './PHPModulePicker'

interface Props {
  projectId: string
  runtimeHint?: string
}

export function ProjectPHPModulesSection({ projectId, runtimeHint = '' }: Props) {
  const { user } = useAuth()
  const readOnly = user?.role === 'viewer'
  const [config, setConfig] = useState<RuntimeContainerConfig | null>(null)
  const [runtime, setRuntime] = useState<ProjectRuntimeInfo | null>(null)
  const [catalog, setCatalog] = useState<RuntimeModuleOption[]>([])
  const [loading, setLoading] = useState(true)
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError('')
    setMessage('')
    Promise.all([
      request<RuntimeContainerConfig>(`/projects/${encodeURIComponent(projectId)}/runtime/config`),
      request<ProjectRuntimeInfo>(`/projects/${encodeURIComponent(projectId)}/runtime`).catch(() => null),
    ])
      .then(([runtimeConfig, runtimeInfo]) => {
        if (cancelled) return
        setConfig(runtimeConfig)
        setRuntime(runtimeInfo)
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : 'Nie udało się wczytać konfiguracji modułów PHP')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [projectId])

  const effectiveRuntime = effectiveRuntimeName(config?.runtime, runtime?.runtime, runtimeHint)

  useEffect(() => {
    if (effectiveRuntime !== 'php') {
      setCatalog([])
      return
    }
    let cancelled = false
    setCatalogLoading(true)
    request<RuntimeModuleOption[]>('/runtimes/php/modules')
      .then((items) => {
        if (!cancelled) setCatalog(items ?? [])
      })
      .catch((reason: unknown) => {
        if (!cancelled) {
          setCatalog([])
          setError(reason instanceof Error ? reason.message : 'Nie udało się wczytać listy modułów PHP')
        }
      })
      .finally(() => {
        if (!cancelled) setCatalogLoading(false)
      })
    return () => { cancelled = true }
  }, [effectiveRuntime])

  const selected = useMemo(() => new Set((config?.modules ?? []).map((item) => item.name)), [config?.modules])
  function toggleModule(name: string, enabled: boolean) {
    setConfig((current) => current
      ? { ...preparePHPModuleConfig(current), modules: updatePHPModuleSelection(current.modules, name, enabled) }
      : current)
    setMessage('')
  }

  function replaceModules(names: string[]) {
    setConfig((current) => current
      ? { ...preparePHPModuleConfig(current), modules: names.map((name) => ({ name })) }
      : current)
    setMessage('')
  }

  async function saveModules(deploy: boolean) {
    if (!config) return
    setBusy(deploy ? 'deploy' : 'save')
    setError('')
    setMessage('')
    try {
      const saved = await request<RuntimeContainerConfig>(`/projects/${encodeURIComponent(projectId)}/runtime/config`, {
        method: 'PUT',
        body: JSON.stringify(preparePHPModuleConfig(config)),
      })
      setConfig(saved)
      if (deploy) {
        const job = await request<Job>(`/projects/${encodeURIComponent(projectId)}/deploy`, {
          method: 'POST',
          body: '{}',
        })
        setMessage(`Moduły PHP zapisane. Deployment został dodany do kolejki: ${job.id.slice(0, 12)}.`)
      } else {
        setMessage('Moduły PHP zapisane. Zostaną użyte przy następnym deploymencie obrazu PHP.')
      }
    } catch (reason: unknown) {
      setError(reason instanceof Error ? reason.message : 'Nie udało się zapisać modułów PHP')
    } finally {
      setBusy('')
    }
  }

  if (loading) {
    return runtimeHint.trim().toLowerCase() === 'php'
      ? <section className="panel runtime-section"><p className="muted">Wczytywanie modułów PHP…</p></section>
      : null
  }
  if (effectiveRuntime !== 'php') return null

  return <section className="panel runtime-section" aria-label="Moduły PHP do deploymentu">
    <div className="section-heading">
      <div>
        <h2>Moduły PHP do deploymentu</h2>
        <p className="muted">Wybierz rozszerzenia, które DevBox ma doinstalować podczas budowania zarządzanego obrazu PHP dla tego projektu.</p>
        <p className="muted small">Wybrane moduły są zapisane per projekt i wpływają na fingerprint obrazu. Zmiana listy wymusi przebudowanie obrazu przy kolejnym deploymencie.</p>
      </div>
      <span className="status-chip" data-ok="true">PHP</span>
    </div>

    {error && <div className="error-banner" role="alert">{error}</div>}
    {message && <div className="success-banner" role="status">{message}</div>}
    {config?.container_policy === 'custom' && <div className="warning-banner">Własny Dockerfile lub Docker Compose pozostaje źródłem prawdy. DevBox nie dopisuje rozszerzeń PHP do plików kontenera należących do projektu.</div>}

    <div className="runtime-modules">
      <div className="runtime-modules-heading">
        <div>
          <h3>Rozszerzenia obrazu PHP</h3>
          <p className="muted">Wybierz potrzebne moduły. Lista jest pogrupowana według zastosowania i sposobu dostarczenia.</p>
        </div>
        <span className="runtime-module-count">{selected.size} wybranych</span>
      </div>

      <PHPModulePicker
        catalog={catalog}
        selected={selected}
        disabled={readOnly || busy !== ''}
        loading={catalogLoading}
        onToggle={toggleModule}
        onSelectionChange={replaceModules}
      />

      {!readOnly && <div className="actions php-module-save-actions">
        <button type="button" disabled={busy !== '' || !config} onClick={() => void saveModules(false)}>
          {busy === 'save' ? 'Zapisywanie…' : 'Zapisz moduły'}
        </button>
        <button type="button" className="secondary" disabled={busy !== '' || !config} onClick={() => void saveModules(true)}>
          {busy === 'deploy' ? 'Dodawanie deploymentu…' : 'Zapisz i wdroż'}
        </button>
      </div>}
    </div>

    <p className="muted small">Dotyczy obrazu PHP generowanego przez DevBox. Jeżeli projekt posiada własny Dockerfile lub Compose, moduły należy także zapewnić w definicji tego kontenera.</p>
  </section>
}

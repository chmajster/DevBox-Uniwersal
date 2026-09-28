import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { request } from '../api/client'
import { DirectoryPicker } from '../components/DirectoryPicker'
import type {
  CentralCredential,
  DeploymentMode,
  Project,
  ProjectSourceType,
  SourceControlBranch,
  SourceControlIntegration,
  SourceControlNamespace,
  SourceControlPageResult,
  SourceControlRepository
} from '../api/types'

type SourceMode = 'github' | 'gitlab' | 'git_url' | 'local' | 'empty'

interface FormState {
  name: string
  description: string
  source_type: ProjectSourceType
  repository_url: string
  branch: string
  local_path: string
  runtime: string
  deployment_mode: DeploymentMode
  working_directory: string
  build_command: string
  start_command: string
  healthcheck: string
  auto_start: boolean
  credential_id: string
  integration_id: string
  repository_path: string
}

const initial: FormState = {
  name: '',
  description: '',
  source_type: 'git',
  repository_url: '',
  branch: 'main',
  local_path: '',
  runtime: '',
  deployment_mode: 'native',
  working_directory: '',
  build_command: '',
  start_command: '',
  healthcheck: '',
  auto_start: false,
  credential_id: '',
  integration_id: '',
  repository_path: ''
}

export function ProjectWizardPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState(1)
  const [form, setForm] = useState<FormState>(initial)
  const [sourceMode, setSourceMode] = useState<SourceMode>('github')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [directoryBrowserOpen, setDirectoryBrowserOpen] = useState(false)
  const [credentials, setCredentials] = useState<CentralCredential[]>([])
  const [integrations, setIntegrations] = useState<SourceControlIntegration[]>([])
  const [namespaces, setNamespaces] = useState<SourceControlNamespace[]>([])
  const [repositories, setRepositories] = useState<SourceControlRepository[]>([])
  const [branches, setBranches] = useState<SourceControlBranch[]>([])
  const [namespaceSearch, setNamespaceSearch] = useState('')
  const [repositorySearch, setRepositorySearch] = useState('')
  const [branchSearch, setBranchSearch] = useState('')
  const [repositoryLoading, setRepositoryLoading] = useState(false)

  const set = <K extends keyof FormState,>(key: K, value: FormState[K]) => setForm((current) => ({ ...current, [key]: value }))

  useEffect(() => {
    request<CentralCredential[]>('/credentials').then((items) => setCredentials(items ?? [])).catch(() => setCredentials([]))
    request<SourceControlIntegration[]>('/integrations').then((items) => setIntegrations(items ?? [])).catch(() => setIntegrations([]))
  }, [])

  const providerIntegrations = useMemo(() => {
    if (sourceMode !== 'github' && sourceMode !== 'gitlab') return []
    return integrations.filter((item) => item.provider === sourceMode && item.enabled && (item.status === 'connected' || item.status === 'degraded'))
  }, [integrations, sourceMode])

  useEffect(() => {
    if (sourceMode !== 'github' && sourceMode !== 'gitlab') return
    const current = providerIntegrations.find((item) => item.id === form.integration_id)
    if (!current && providerIntegrations.length > 0) {
      setForm((value) => ({ ...value, integration_id: providerIntegrations[0].id, repository_path: '', repository_url: '', branch: '' }))
    }
  }, [sourceMode, providerIntegrations, form.integration_id])

  useEffect(() => {
    if (!form.integration_id || (sourceMode !== 'github' && sourceMode !== 'gitlab')) {
      setNamespaces([])
      return
    }
    const timer = window.setTimeout(() => {
      request<SourceControlPageResult<SourceControlNamespace>>(
        `/integrations/${encodeURIComponent(form.integration_id)}/namespaces?search=${encodeURIComponent(namespaceSearch)}&page=1&per_page=50`
      ).then((result) => setNamespaces(result.items)).catch(() => setNamespaces([]))
    }, 250)
    return () => window.clearTimeout(timer)
  }, [form.integration_id, namespaceSearch, sourceMode])

  useEffect(() => {
    if (!form.integration_id || (sourceMode !== 'github' && sourceMode !== 'gitlab')) {
      setRepositories([])
      return
    }
    const timer = window.setTimeout(() => {
      setRepositoryLoading(true)
      const query = new URLSearchParams({ search: repositorySearch, page: '1', per_page: '50' })
      if (namespaceSearch.trim()) query.set('namespace', namespaceSearch.trim())
      request<SourceControlPageResult<SourceControlRepository>>(
        `/integrations/${encodeURIComponent(form.integration_id)}/repositories?${query.toString()}`
      ).then((result) => setRepositories(result.items)).catch(() => setRepositories([])).finally(() => setRepositoryLoading(false))
    }, 300)
    return () => window.clearTimeout(timer)
  }, [form.integration_id, namespaceSearch, repositorySearch, sourceMode])

  useEffect(() => {
    if (!form.integration_id || !form.repository_path || (sourceMode !== 'github' && sourceMode !== 'gitlab')) {
      setBranches([])
      return
    }
    const timer = window.setTimeout(() => {
      const query = new URLSearchParams({ repository: form.repository_path, search: branchSearch, page: '1', per_page: '50' })
      request<SourceControlPageResult<SourceControlBranch>>(
        `/integrations/${encodeURIComponent(form.integration_id)}/branches?${query.toString()}`
      ).then((result) => setBranches(result.items)).catch(() => setBranches([]))
    }, 250)
    return () => window.clearTimeout(timer)
  }, [form.integration_id, form.repository_path, branchSearch, sourceMode])

  function changeSourceMode(mode: SourceMode) {
    setSourceMode(mode)
    setDirectoryBrowserOpen(false)
    setNamespaceSearch('')
    setRepositorySearch('')
    setBranchSearch('')
    if (mode === 'local') {
      setForm((current) => ({ ...current, source_type: 'local', repository_url: '', integration_id: '', repository_path: '', credential_id: '' }))
    } else if (mode === 'empty') {
      setForm((current) => ({ ...current, source_type: 'empty', repository_url: '', integration_id: '', repository_path: '', credential_id: '' }))
    } else {
      setForm((current) => ({ ...current, source_type: 'git', repository_url: '', integration_id: '', repository_path: '', branch: mode === 'git_url' ? 'main' : '', credential_id: mode === 'git_url' ? current.credential_id : '' }))
    }
  }

  function chooseRepository(repository: SourceControlRepository) {
    const derivedName = repository.name.replace(/\.git$/i, '')
    setForm((current) => ({
      ...current,
      repository_path: repository.path,
      repository_url: repository.clone_url,
      branch: repository.default_branch || 'main',
      name: current.name || derivedName
    }))
    setRepositorySearch(repository.path)
    setBranchSearch(repository.default_branch || 'main')
  }

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (step < 3) {
      setStep(step + 1)
      return
    }
    setBusy(true)
    setError('')
    try {
      const path = form.source_type === 'local' ? '/projects/import' : '/projects'
      const project = await request<Project>(path, { method: 'POST', body: JSON.stringify(form) })
      navigate(`/apps/${project.id}`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to create application')
    } finally {
      setBusy(false)
    }
  }

  const integrationSource = sourceMode === 'github' || sourceMode === 'gitlab'

  return <form className="wizard" onSubmit={submit}>
    <div className="page-heading"><div><h1>Dodaj aplikację</h1><p className="muted">Krok {step} z 3</p></div></div>
    {error && <div className="error-banner">{error}</div>}

    {step === 1 && <div className="panel form-grid">
      <label>Nazwa
        <input value={form.name} onChange={(event) => set('name', event.target.value)} required={sourceMode === 'local' || sourceMode === 'empty'} placeholder={integrationSource || sourceMode === 'git_url' ? 'Opcjonalna — domyślnie nazwa repo' : ''} />
      </label>
      <label>Source
        <select value={sourceMode} onChange={(event) => changeSourceMode(event.target.value as SourceMode)}>
          <option value="github">GitHub</option>
          <option value="gitlab">GitLab</option>
          <option value="git_url">Git URL</option>
          <option value="local">Local directory</option>
          <option value="empty">Empty project</option>
        </select>
      </label>
      <label className="span-2">Opis<textarea value={form.description} onChange={(event) => set('description', event.target.value)} /></label>

      {integrationSource && <>
        <label>Integration
          <select value={form.integration_id} onChange={(event) => {
            set('integration_id', event.target.value)
            set('repository_path', '')
            setRepositorySearch('')
            set('branch', '')
          }} required>
            <option value="">Select integration</option>
            {providerIntegrations.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.account_username || item.web_url}</option>)}
          </select>
        </label>
        <label>Organization / Group
          <input list="source-namespaces" value={namespaceSearch} onChange={(event) => setNamespaceSearch(event.target.value)} placeholder="All accessible namespaces" />
          <datalist id="source-namespaces">{namespaces.map((item) => <option value={item.path} key={item.id}>{item.kind}</option>)}</datalist>
        </label>
        <div className="span-2">
          <label>Repository search
            <input value={repositorySearch} onChange={(event) => {
              setRepositorySearch(event.target.value)
              if (event.target.value !== form.repository_path) set('repository_path', '')
            }} placeholder="Search accessible repositories…" required={!form.repository_path} />
          </label>
          <div className="repository-results" role="listbox" aria-label="Repositories">
            {repositoryLoading && <span className="muted">Loading repositories…</span>}
            {!repositoryLoading && repositories.map((repository) => <button
              type="button"
              className={`repository-result ${repository.path === form.repository_path ? 'selected' : ''}`}
              key={repository.id}
              onClick={() => chooseRepository(repository)}
            >
              <strong>{repository.path}</strong>
              <span>{repository.visibility || 'repository'} · default {repository.default_branch || '—'}</span>
            </button>)}
            {!repositoryLoading && form.integration_id && repositories.length === 0 && <span className="muted">No repositories match the current search.</span>}
          </div>
        </div>
        <label>Branch
          <input list="source-branches" value={form.branch} onChange={(event) => { set('branch', event.target.value); setBranchSearch(event.target.value) }} disabled={!form.repository_path} required />
          <datalist id="source-branches">{branches.map((branch) => <option value={branch.name} key={branch.name}>{branch.default ? 'default' : branch.protected ? 'protected' : ''}</option>)}</datalist>
        </label>
        <div className="source-selected">
          <span className="muted">Selected repository</span>
          <strong>{form.repository_path || 'None'}</strong>
        </div>
        {providerIntegrations.length === 0 && <div className="span-2 warning-banner">
          Brak aktywnej integracji {sourceMode === 'github' ? 'GitHub' : 'GitLab'}. <Link to="/integrations">Skonfiguruj Integrations</Link>.
        </div>}
      </>}

      {sourceMode === 'git_url' && <>
        <label className="span-2">Repository URL
          <input value={form.repository_url} onChange={(event) => set('repository_url', event.target.value)} required placeholder="https://github.com/example/my-project.git" />
        </label>
        <label>Branch<input value={form.branch} onChange={(event) => set('branch', event.target.value)} /></label>
        <label>Poświadczenie
          <select value={form.credential_id} onChange={(event) => set('credential_id', event.target.value)}>
            <option value="">Brak / public repo</option>
            {credentials.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.kind === 'token' ? 'token' : 'SSH key'}</option>)}
          </select>
        </label>
        {credentials.length === 0 && <div className="span-2 muted small">Brak centralnych poświadczeń. <Link to="/credentials">Dodaj poświadczenie</Link>.</div>}
      </>}

      {sourceMode === 'local' && <>
        <div className="span-2 path-picker-field">
          <label htmlFor="local-path">Pełna ścieżka katalogu</label>
          <div className="path-picker-row">
            <input id="local-path" value={form.local_path} onChange={(event) => set('local_path', event.target.value)} required />
            <button type="button" className="secondary" onClick={() => setDirectoryBrowserOpen((open) => !open)}>{directoryBrowserOpen ? 'Ukryj drzewko' : 'Przeglądaj…'}</button>
          </div>
        </div>
        {directoryBrowserOpen && <div className="span-2"><DirectoryPicker value={form.local_path} onSelect={(path) => set('local_path', path)} onClose={() => setDirectoryBrowserOpen(false)} /></div>}
      </>}
    </div>}

    {step === 2 && <div className="panel form-grid">
      <label>Runtime<input value={form.runtime} onChange={(event) => set('runtime', event.target.value)} placeholder="np. go, python, php" /></label>
      <label>Deployment mode<select value={form.deployment_mode} onChange={(event) => set('deployment_mode', event.target.value as DeploymentMode)}><option value="native">native</option><option value="docker">docker</option></select></label>
      <label className="span-2">Working directory<input value={form.working_directory} onChange={(event) => set('working_directory', event.target.value)} /></label>
      <label className="span-2">Build command<input value={form.build_command} onChange={(event) => set('build_command', event.target.value)} /></label>
      <label className="span-2">Start command<input value={form.start_command} onChange={(event) => set('start_command', event.target.value)} /></label>
      <label className="span-2">Healthcheck<input value={form.healthcheck} onChange={(event) => set('healthcheck', event.target.value)} /></label>
      <label className="checkbox"><input type="checkbox" checked={form.auto_start} onChange={(event) => set('auto_start', event.target.checked)} /> Auto start</label>
    </div>}

    {step === 3 && <div className="panel summary-grid">
      <div><span>Nazwa</span><strong>{form.name || form.repository_path.split('/').pop()?.replace(/\.git$/i, '') || form.repository_url.split('/').pop()?.replace(/\.git$/i, '') || '—'}</strong></div>
      <div><span>Źródło</span><strong>{sourceMode}</strong></div>
      <div><span>Runtime</span><strong>{form.runtime || 'auto-detect'}</strong></div>
      <div><span>Tryb</span><strong>{form.deployment_mode}</strong></div>
      {integrationSource && <div className="span-2"><span>Repository</span><strong>{form.repository_path} · {form.branch || 'default'}</strong></div>}
      {sourceMode === 'git_url' && <div className="span-2"><span>Repo</span><strong>{form.repository_url} · {form.branch || 'default'}</strong></div>}
      {sourceMode === 'local' && <div className="span-2"><span>Katalog</span><strong>{form.local_path}</strong></div>}
    </div>}

    <div className="wizard-actions">
      {step > 1 && <button type="button" className="secondary" onClick={() => setStep(step - 1)}>Wstecz</button>}
      <button type="submit" disabled={busy || (step === 1 && integrationSource && (!form.integration_id || !form.repository_path))}>
        {step < 3 ? 'Dalej' : busy ? 'Zapisywanie…' : 'Utwórz aplikację'}
      </button>
    </div>
  </form>
}

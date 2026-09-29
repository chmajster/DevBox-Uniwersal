import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { request } from '../api/client'
import { DirectoryPicker } from '../components/DirectoryPicker'
import type { CentralCredential, Project, ProjectSourceType } from '../api/types'
import { GitIntegrationBrowser } from '../git/GitIntegrationBrowser'
import { repositoryNameFromURL } from './projectWizardHelpers'

interface FormState {
  name: string
  description: string
  source_type: ProjectSourceType
  repository_url: string
  branch: string
  local_path: string
  runtime: string
  runtime_version: string
  container_policy: 'auto' | 'custom'
  working_directory: string
  build_command: string
  start_command: string
  healthcheck: string
  auto_start: boolean
  credential_id: string
}

const initial: FormState = {
  name: '',
  description: '',
  source_type: 'git',
  repository_url: '',
  branch: '',
  local_path: '',
  runtime: '',
  runtime_version: '',
  container_policy: 'auto',
  working_directory: '',
  build_command: '',
  start_command: '',
  healthcheck: '',
  auto_start: false,
  credential_id: '',
}

const runtimeOptions = [
  { value: '', label: 'Automatycznie wykryj po sklonowaniu' },
  { value: 'php', label: 'PHP' },
  { value: 'node', label: 'Node.js' },
  { value: 'python', label: 'Python' },
  { value: 'go', label: 'Go' },
  { value: 'static', label: 'Static / HTML' },
] as const

export function ProjectWizardPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState(1)
  const [form, setForm] = useState<FormState>(initial)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [directoryBrowserOpen, setDirectoryBrowserOpen] = useState(false)
  const [credentials, setCredentials] = useState<CentralCredential[]>([])
  const [nameEdited, setNameEdited] = useState(false)

  const set = <K extends keyof FormState,>(key: K, value: FormState[K]) =>
    setForm((current) => ({ ...current, [key]: value }))

  useEffect(() => {
    request<CentralCredential[]>('/credentials')
      .then((items) => setCredentials(items ?? []))
      .catch(() => setCredentials([]))
  }, [])

  function changeRepositoryURL(value: string) {
    setForm((current) => {
      const suggestedName = repositoryNameFromURL(value)
      return {
        ...current,
        repository_url: value,
        name: !nameEdited && suggestedName ? suggestedName : current.name,
      }
    })
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
      const project = await request<Project>(path, {
        method: 'POST',
        body: JSON.stringify(form),
      })
      navigate(`/apps/${project.id}`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to create application')
    } finally {
      setBusy(false)
    }
  }

  const finalButtonLabel = form.source_type === 'git' ? 'Sklonuj aplikację' : 'Utwórz aplikację'

  return <form className="wizard" onSubmit={submit}>
    <div className="page-heading">
      <div>
        <h1>Dodaj aplikację</h1>
        <p className="muted">Krok {step} z 3</p>
      </div>
    </div>

    {error && <div className="error-banner">{error}</div>}

    {step === 1 && <div className="panel form-grid">
      <label>Nazwa
        <input
          value={form.name}
          onChange={(event) => {
            setNameEdited(true)
            set('name', event.target.value)
          }}
          placeholder={form.source_type === 'git' ? 'Uzupełni się z nazwy repozytorium' : ''}
          required
        />
      </label>

      <label>Typ źródła
        <select
          value={form.source_type}
          onChange={(event) => {
            const source = event.target.value as ProjectSourceType
            set('source_type', source)
            if (source !== 'local') setDirectoryBrowserOpen(false)
          }}
        >
          <option value="git">Git repository</option>
          <option value="local">Local directory</option>
          <option value="empty">Empty project</option>
        </select>
      </label>

      <label className="span-2">Opis
        <textarea value={form.description} onChange={(event) => set('description', event.target.value)} />
      </label>

      {form.source_type === 'git' && <>
        <GitIntegrationBrowser credentials={credentials} onSelect={(repo, branch, credentialId) => setForm((current) => ({ ...current, repository_url: repo.clone_url, branch, credential_id: credentialId, name: nameEdited ? current.name : repositoryNameFromURL(repo.clone_url) }))} />
        <label className="span-2">Git URL
          <input
            value={form.repository_url}
            onChange={(event) => changeRepositoryURL(event.target.value)}
            placeholder="https://github.com/uzytkownik/moj-projekt.git"
            autoComplete="url"
            required
          />
          <span className="muted small">Po utworzeniu DevBox wykona prawdziwy git clone do zarządzanego katalogu projektu.</span>
        </label>

        <label>Branch
          <input
            value={form.branch}
            onChange={(event) => set('branch', event.target.value)}
            placeholder="puste = domyślna gałąź repozytorium"
          />
        </label>

        <label>Poświadczenie
          <select value={form.credential_id} onChange={(event) => set('credential_id', event.target.value)}>
            <option value="">Brak / repo publiczne</option>
            {credentials.map((item) =>
              <option key={item.id} value={item.id}>
                {item.name} · {item.kind === 'token' ? 'Token GitHub / GitLab' : 'SSH key'}
              </option>
            )}
          </select>
        </label>

        {credentials.length === 0 && <div className="span-2 muted small">
          Dla prywatnego repozytorium dodaj centralne poświadczenie w <Link to="/credentials">Danych dostępowych</Link>.
        </div>}
      </>}

      {form.source_type === 'local' && <>
        <div className="span-2 path-picker-field">
          <label htmlFor="local-path">Pełna ścieżka katalogu</label>
          <div className="path-picker-row">
            <input id="local-path" value={form.local_path} onChange={(event) => set('local_path', event.target.value)} required />
            <button type="button" className="secondary" onClick={() => setDirectoryBrowserOpen((open) => !open)}>
              {directoryBrowserOpen ? 'Ukryj drzewko' : 'Przeglądaj…'}
            </button>
          </div>
        </div>
        {directoryBrowserOpen && <div className="span-2">
          <DirectoryPicker
            value={form.local_path}
            onSelect={(path) => set('local_path', path)}
            onClose={() => setDirectoryBrowserOpen(false)}
          />
        </div>}
      </>}
    </div>}

    {step === 2 && <div className="panel form-grid">
      <label>Typ aplikacji / runtime
        <select value={form.runtime} onChange={(event) => set('runtime', event.target.value)}>
          {runtimeOptions.map((runtime) =>
            <option key={runtime.value || 'auto'} value={runtime.value}>{runtime.label}</option>
          )}
        </select>
      </label>

      <label>Wersja runtime
        <input value={form.runtime_version} onChange={(event) => set('runtime_version', event.target.value)} placeholder="puste = wersja domyślna obrazu" disabled={!form.runtime} />
      </label>

      <label>Kontener
        <select value={form.container_policy} onChange={(event) => set('container_policy', event.target.value as 'auto' | 'custom')}>
          <option value="auto">Automatycznie: użyj Dockerfile/Compose lub wygeneruj kontener</option>
          <option value="custom">Tylko własny Dockerfile / Compose</option>
        </select>
      </label>

      <label className="span-2">Working directory
        <input value={form.working_directory} onChange={(event) => set('working_directory', event.target.value)} placeholder="puste = katalog główny repo" />
      </label>

      <label className="span-2">Build command
        <input value={form.build_command} onChange={(event) => set('build_command', event.target.value)} />
      </label>

      <label className="span-2">Start command
        <input value={form.start_command} onChange={(event) => set('start_command', event.target.value)} />
      </label>

      <label className="span-2">Healthcheck
        <input value={form.healthcheck} onChange={(event) => set('healthcheck', event.target.value)} />
      </label>

      <label className="checkbox">
        <input type="checkbox" checked={form.auto_start} onChange={(event) => set('auto_start', event.target.checked)} />
        Auto start
      </label>

      {form.source_type === 'git' && <div className="span-2 muted small">
        Przykład: wybierz PHP. DevBox zapisze runtime jako PHP i po utworzeniu projektu uruchomi zadanie klonowania Git. Puste pole runtime pozostawia automatyczną detekcję. Runtime aplikacji zawsze działa w kontenerze; DevBox nie instaluje PHP/Go/Node/Pythona na hoście.
      </div>}
    </div>}

    {step === 3 && <div className="panel summary-grid">
      <div><span>Nazwa</span><strong>{form.name}</strong></div>
      <div><span>Źródło</span><strong>{form.source_type}</strong></div>
      <div><span>Runtime</span><strong>{form.runtime || 'automatyczne wykrywanie'}</strong></div>
      <div><span>Kontener</span><strong>{form.container_policy === 'auto' ? 'automatyczny' : 'własny Docker'}</strong></div>
      {form.source_type === 'git' && <>
        <GitIntegrationBrowser credentials={credentials} onSelect={(repo, branch, credentialId) => setForm((current) => ({ ...current, repository_url: repo.clone_url, branch, credential_id: credentialId, name: nameEdited ? current.name : repositoryNameFromURL(repo.clone_url) }))} />
        <div className="span-2"><span>Repo</span><strong>{form.repository_url}</strong></div>
        <div><span>Branch</span><strong>{form.branch || 'domyślna gałąź repo'}</strong></div>
        <div><span>Operacja</span><strong>git clone</strong></div>
      </>}
      {form.source_type === 'local' && <div className="span-2"><span>Katalog</span><strong>{form.local_path}</strong></div>}
    </div>}

    <div className="wizard-actions">
      {step > 1 && <button type="button" className="secondary" onClick={() => setStep(step - 1)}>Wstecz</button>}
      <button type="submit" disabled={busy}>
        {step < 3 ? 'Dalej' : busy ? (form.source_type === 'git' ? 'Uruchamianie klonowania…' : 'Zapisywanie…') : finalButtonLabel}
      </button>
    </div>
  </form>
}

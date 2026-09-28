import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { request } from '../api/client'
import type { CentralCredential, Job, SourceControlIntegration, SourceControlProviderName } from '../api/types'
import { useAuth } from '../auth/AuthContext'

interface IntegrationForm {
  name: string
  provider: SourceControlProviderName
  web_url: string
  api_url: string
  credential_id: string
  enabled: boolean
}

const defaults: Record<SourceControlProviderName, Pick<IntegrationForm, 'web_url' | 'api_url'>> = {
  github: { web_url: 'https://github.com', api_url: 'https://api.github.com' },
  gitlab: { web_url: 'https://gitlab.com', api_url: 'https://gitlab.com/api/v4' }
}

function emptyForm(provider: SourceControlProviderName = 'github'): IntegrationForm {
  return { name: '', provider, credential_id: '', enabled: true, ...defaults[provider] }
}

export function IntegrationsPage() {
  const { user } = useAuth()
  const [items, setItems] = useState<SourceControlIntegration[]>([])
  const [credentials, setCredentials] = useState<CentralCredential[]>([])
  const [form, setForm] = useState<IntegrationForm>(emptyForm())
  const [editing, setEditing] = useState<SourceControlIntegration | null>(null)
  const [query, setQuery] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const load = useCallback(async () => {
    const integrations = await request<SourceControlIntegration[]>('/integrations')
    setItems(integrations)
  }, [])

  useEffect(() => {
    load().catch((reason: unknown) => setError(reason instanceof Error ? reason.message : 'Nie udało się pobrać integracji'))
    if (user?.role === 'admin') {
      request<CentralCredential[]>('/credentials').then(setCredentials).catch(() => setCredentials([]))
    }
  }, [load, user?.role])

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    if (!needle) return items
    return items.filter((item) => [item.name, item.provider, item.account_username, item.web_url, item.status].some((value) => value?.toLowerCase().includes(needle)))
  }, [items, query])

  function setProvider(provider: SourceControlProviderName) {
    setForm((current) => ({ ...current, provider, ...defaults[provider] }))
  }

  function beginEdit(item: SourceControlIntegration) {
    setEditing(item)
    setForm({
      name: item.name,
      provider: item.provider,
      web_url: item.web_url,
      api_url: item.api_url,
      credential_id: item.credential_id,
      enabled: item.enabled
    })
    setError('')
    setMessage('')
  }

  function resetForm() {
    setEditing(null)
    setForm(emptyForm())
  }

  async function save(event: FormEvent) {
    event.preventDefault()
    setBusy('save')
    setError('')
    setMessage('')
    try {
      const path = editing ? `/integrations/${encodeURIComponent(editing.id)}` : '/integrations'
      const method = editing ? 'PUT' : 'POST'
      const result = await request<SourceControlIntegration>(path, { method, body: JSON.stringify(form) })
      setMessage(`Połączenie zweryfikowane. Connected as: ${result.account_username || 'unknown'}.`)
      resetForm()
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Nie udało się zapisać integracji')
    } finally {
      setBusy('')
    }
  }

  async function action(item: SourceControlIntegration, action: 'test' | 'sync') {
    setBusy(`${action}:${item.id}`)
    setError('')
    setMessage('')
    try {
      if (action === 'test') {
        const result = await request<{ connected: boolean; status: string; account?: string; error?: string }>(`/integrations/${encodeURIComponent(item.id)}/test`, { method: 'POST', body: '{}' })
        setMessage(result.connected ? `Connected as: ${result.account || 'unknown'}.` : `Connection status: ${result.status}. ${result.error || ''}`)
      } else {
        const job = await request<Job>(`/integrations/${encodeURIComponent(item.id)}/sync`, { method: 'POST', body: '{}' })
        setMessage(`Sync uruchomiony jako job ${job.id}.`)
      }
      await load()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : `Operacja ${action} nie powiodła się`)
    } finally {
      setBusy('')
    }
  }

  async function remove(item: SourceControlIntegration) {
    if (!window.confirm(`Usunąć integrację „${item.name}”? Integracji używanej przez projekt backend nie usunie.`)) return
    setBusy(`delete:${item.id}`)
    setError('')
    setMessage('')
    try {
      await request<unknown>(`/integrations/${encodeURIComponent(item.id)}`, { method: 'DELETE' })
      await load()
      setMessage('Integracja usunięta.')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Nie udało się usunąć integracji')
    } finally {
      setBusy('')
    }
  }

  return <>
    <div className="page-heading">
      <div>
        <h1>Integrations</h1>
        <p className="muted">GitHub, GitHub Enterprise Server, GitLab i self-hosted GitLab. Token jest pobierany wyłącznie z centralnego SecretStore.</p>
      </div>
      <input className="integration-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Szukaj integracji…" aria-label="Szukaj integracji" />
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}

    {user?.role === 'admin' && <form className="panel form-grid integration-form" onSubmit={save}>
      <div className="span-2">
        <h2>{editing ? `Edit: ${editing.name}` : 'Add integration'}</h2>
        <p className="muted">Zapisanie konfiguracji wymaga rzeczywistego connection test. Nieudane uwierzytelnienie nie zostanie oznaczone jako Connected.</p>
      </div>
      <label>Name<input value={form.name} onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))} required /></label>
      <label>Provider<select value={form.provider} onChange={(event) => setProvider(event.target.value as SourceControlProviderName)}>
        <option value="github">GitHub</option>
        <option value="gitlab">GitLab</option>
      </select></label>
      <label>Web URL<input value={form.web_url} onChange={(event) => setForm((current) => ({ ...current, web_url: event.target.value }))} required /></label>
      <label>API URL<input value={form.api_url} onChange={(event) => setForm((current) => ({ ...current, api_url: event.target.value }))} required /></label>
      <label>Central Credential<select value={form.credential_id} onChange={(event) => setForm((current) => ({ ...current, credential_id: event.target.value }))} required>
        <option value="">Select credential</option>
        {credentials.filter((credential) => credential.kind === 'token').map((credential) => <option value={credential.id} key={credential.id}>{credential.name} · token</option>)}
      </select></label>
      <label className="checkbox"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm((current) => ({ ...current, enabled: event.target.checked }))} /> Enabled</label>
      <div className="form-actions">
        {editing && <button type="button" className="secondary" onClick={resetForm}>Cancel</button>}
        <button type="submit" disabled={busy === 'save' || !form.credential_id}>{busy === 'save' ? 'Testing connection…' : editing ? 'Test and save' : 'Test and add'}</button>
      </div>
    </form>}

    <div className="integration-grid">
      {visible.map((item) => <article className="panel integration-card" key={item.id}>
        <div className="runtime-version-heading">
          <div>
            <h2>{item.name}</h2>
            <p className="muted">{item.provider === 'github' ? 'GitHub' : 'GitLab'} · {item.web_url}</p>
          </div>
          <span className={`badge ${item.status === 'connected' ? 'badge-ok' : item.status === 'degraded' ? 'badge-warn' : 'badge-muted'}`}>{item.status}</span>
        </div>
        <div className="detail-grid integration-metrics">
          <div className="detail-field"><span>Account</span><strong>{item.account_username || '—'}</strong></div>
          <div className="detail-field"><span>Repositories</span><strong>{item.repository_count ?? 0}</strong></div>
          <div className="detail-field"><span>Namespaces</span><strong>{item.namespace_count ?? 0}</strong></div>
          <div className="detail-field"><span>Last tested</span><strong>{item.last_tested_at ? new Date(item.last_tested_at).toLocaleString() : '—'}</strong></div>
          <div className="detail-field"><span>Last sync</span><strong>{item.last_synced_at ? new Date(item.last_synced_at).toLocaleString() : '—'}</strong></div>
          <div className="detail-field"><span>Credential</span><strong className="mono">{item.credential_id}</strong><small>Secret: ••••••••••••</small></div>
        </div>
        {item.scopes && item.scopes.length > 0 && <p className="muted">Scopes: {item.scopes.join(', ')}</p>}
        {user?.role === 'admin' && <div className="actions">
          <button type="button" className="secondary" disabled={Boolean(busy)} onClick={() => void action(item, 'test')}>Test connection</button>
          <button type="button" className="secondary" disabled={Boolean(busy)} onClick={() => void action(item, 'sync')}>Sync</button>
          <button type="button" className="secondary" disabled={Boolean(busy)} onClick={() => beginEdit(item)}>Edit</button>
          <button type="button" className="danger" disabled={Boolean(busy)} onClick={() => void remove(item)}>Delete</button>
        </div>}
      </article>)}
      {visible.length === 0 && <div className="panel empty-panel">Brak integracji pasujących do filtra.</div>}
    </div>
  </>
}

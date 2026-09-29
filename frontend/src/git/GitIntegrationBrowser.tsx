import { useCallback, useEffect, useState } from 'react'
import { request } from '../api/client'
import type { CentralCredential } from '../api/types'
import { useAuth } from '../auth/AuthContext'

interface Integration { id: string; name: string; provider: 'github' | 'gitlab'; base_url: string; credential_id: string }
export interface GitRepository { id: string; name: string; clone_url: string; url: string; default_branch: string; private: boolean }
interface Branch { name: string; sha: string; protected: boolean }
interface Activity { id: number; title: string; status: string; url: string; branch: string }
interface Overview { requests: Activity[]; pipelines: Activity[]; warnings: string[] }
interface Identity { username: string; scopes: string[]; scope_note: string; repositories_readable: boolean }
interface Props { credentials: CentralCredential[]; manage?: boolean; onSelect?: (repo: GitRepository, branch: string, credentialId: string) => void }

export function GitIntegrationBrowser({ credentials, manage = false, onSelect }: Props) {
  const { user } = useAuth()
  const [profiles, setProfiles] = useState<Integration[]>([])
  const [profileId, setProfileId] = useState('')
  const [name, setName] = useState('')
  const [provider, setProvider] = useState<'github' | 'gitlab'>('github')
  const [baseURL, setBaseURL] = useState('https://api.github.com')
  const [credentialId, setCredentialId] = useState('')
  const [repos, setRepos] = useState<GitRepository[]>([])
  const [repositoryId, setRepositoryId] = useState('')
  const [page, setPage] = useState(1)
  const [branches, setBranches] = useState<Branch[]>([])
  const [branchPage, setBranchPage] = useState(1)
  const [hasMoreBranches, setHasMoreBranches] = useState(false)
  const [branch, setBranch] = useState('')
  const [overview, setOverview] = useState<Overview | null>(null)
  const [identity, setIdentity] = useState<Identity | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const profile = profiles.find((item) => item.id === profileId)
  const repository = repos.find((item) => item.id === repositoryId)
  const loadProfiles = useCallback(async () => setProfiles(await request<Integration[]>('/git-integrations')), [])
  useEffect(() => { void loadProfiles().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Odczyt integracji nie powiódł się')) }, [loadProfiles])
  useEffect(() => {
    setRepos([]); setRepositoryId(''); setBranches([]); setBranch(''); setOverview(null); setIdentity(null)
    if (!profileId) return
    const controller = new AbortController(); setLoading(true); setError('')
    void request<GitRepository[]>(`/git-integrations/${profileId}/repositories?page=${page}`, { signal: controller.signal }).then(setRepos).catch((cause: unknown) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Odczyt repozytoriów nie powiódł się') }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [profileId, page])
  useEffect(() => {
    setBranches([]); setBranchPage(1); setHasMoreBranches(false); setOverview(null)
    if (!repositoryId || !profileId) return
    const controller = new AbortController()
    void request<Branch[]>(`/git-integrations/${profileId}/branches?repository=${encodeURIComponent(repositoryId)}&page=1`, { signal: controller.signal }).then((items) => { setBranches(items); setHasMoreBranches(items.length === 100) }).catch((cause: unknown) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Odczyt gałęzi nie powiódł się') })
    return () => controller.abort()
  }, [repositoryId, profileId])
  async function perform(action: () => Promise<void>) {
    setError(''); setBusy(true)
    try { await action() } catch (cause) { setError(cause instanceof Error ? cause.message : 'Operacja integracji nie powiodła się') } finally { setBusy(false) }
  }
  function createProfile() {
    return perform(async () => {
      const item = await request<Integration>('/git-integrations', { method: 'POST', body: JSON.stringify({ name, provider, base_url: baseURL, credential_id: credentialId }) })
      await loadProfiles(); setProfileId(item.id); setPage(1); setName('')
    })
  }
  function testProfile() { return perform(async () => setIdentity(await request<Identity>(`/git-integrations/${profileId}/test`))) }
  function readOverview() { return perform(async () => setOverview(await request<Overview>(`/git-integrations/${profileId}/overview?repository=${encodeURIComponent(repositoryId)}&branch=${encodeURIComponent(branch)}`))) }
  function moreBranches() {
    return perform(async () => {
      const next = branchPage + 1
      const items = await request<Branch[]>(`/git-integrations/${profileId}/branches?repository=${encodeURIComponent(repositoryId)}&page=${next}`)
      setBranches((current) => [...current, ...items]); setBranchPage(next); setHasMoreBranches(items.length === 100)
    })
  }
  async function deleteProfile() {
    if (!profile || !window.confirm(`Usunąć integrację „${profile.name}”? Token pozostanie w magazynie poświadczeń.`)) return
    await perform(async () => { await request(`/git-integrations/${profileId}`, { method: 'DELETE' }); setProfileId(''); await loadProfiles() })
  }
  return <section className="panel span-2">
    <h2>Integracje GitHub / GitLab</h2>
    <p className="muted">Repozytoria, gałęzie, PR/MR i CI są odczytywane bezpośrednio z API wybranego serwera. Token pozostaje w SecretStore.</p>
    {error && <div className="error-banner">{error}</div>}
    {manage && user?.role === 'admin' && <fieldset disabled={busy}>
      <legend>Nowa integracja</legend>
      <div className="form-grid">
        <label>Nazwa<input value={name} onChange={(e) => setName(e.target.value)} /></label>
        <label>Dostawca<select value={provider} onChange={(e) => { const value = e.target.value as 'github' | 'gitlab'; setProvider(value); setBaseURL(value === 'github' ? 'https://api.github.com' : 'https://gitlab.com') }}><option value="github">GitHub</option><option value="gitlab">GitLab / Self-Managed</option></select></label>
        <label>Zaufany adres serwera HTTPS<input disabled={provider === 'github'} value={baseURL} onChange={(e) => setBaseURL(e.target.value)} /></label>
        <label>Token z magazynu<select value={credentialId} onChange={(e) => setCredentialId(e.target.value)}><option value="">Wybierz token</option>{credentials.filter((item) => item.kind === 'token').map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      </div>
      <p className="muted small">Zapisanie integracji zezwala na wysyłanie wybranego tokenu do tego serwera. Przekierowania do innych adresów są blokowane. Wymagane uprawnienia: odczyt repozytoriów i gałęzi; dodatkowo Actions lub pipelines dla CI.</p>
      <button type="button" disabled={!name.trim() || !credentialId} onClick={() => void createProfile()}>Zapisz integrację</button>
    </fieldset>}
    <div className="form-grid">
      <label>Integracja<select value={profileId} disabled={busy} onChange={(e) => { setPage(1); setProfileId(e.target.value) }}><option value="">Wybierz integrację</option>{profiles.map((item) => <option key={item.id} value={item.id}>{item.name} — {item.base_url}</option>)}</select></label>
      <div className="actions">{profile && <><button type="button" disabled={busy} onClick={() => void testProfile()}>Sprawdź token i dostęp</button>{manage && user?.role === 'admin' && <button type="button" className="danger" disabled={busy} onClick={() => void deleteProfile()}>Usuń integrację</button>}</>}</div>
    </div>
    {identity && <div className="validation-box"><strong>Konto: {identity.username}</strong><p>Dostęp do listy repozytoriów: {identity.repositories_readable ? 'potwierdzony' : 'niepotwierdzony'}. Zwrócone zakresy: {identity.scopes.join(', ') || 'serwer ich nie wylicza'}.</p><p className="muted small">Zakresy tokenów fine-grained mogą nie być zwracane przez API. Odczyt listy repozytoriów nie potwierdza uprawnień do wszystkich operacji.</p></div>}
    {profile && <>
      <label>Repozytorium<select value={repositoryId} disabled={loading || busy} onChange={(e) => { setRepositoryId(e.target.value); setBranch(repos.find((item) => item.id === e.target.value)?.default_branch ?? '') }}><option value="">{loading ? 'Pobieranie…' : 'Wybierz repozytorium'}</option>{repos.map((item) => <option key={item.id} value={item.id}>{item.name}{item.private ? ' (prywatne)' : ''}</option>)}</select></label>
      <div className="actions"><button type="button" className="secondary" disabled={page <= 1 || loading || busy} onClick={() => setPage(page - 1)}>Poprzednia strona</button><span>Strona {page}</span><button type="button" className="secondary" disabled={repos.length < 100 || loading || busy} onClick={() => setPage(page + 1)}>Następna strona</button></div>
    </>}
    {repository && <>
      <p><a href={repository.url} target="_blank" rel="noreferrer">{repository.name}</a> · <code>{repository.clone_url}</code></p>
      <label>Gałąź<input list="integration-branches" value={branch} onChange={(e) => { setBranch(e.target.value); setOverview(null) }} /><datalist id="integration-branches">{branches.map((item) => <option key={item.name} value={item.name}>{item.protected ? 'chroniona' : ''}</option>)}</datalist></label>
      <div className="actions">
        {hasMoreBranches && <button type="button" className="secondary" disabled={busy} onClick={() => void moreBranches()}>Pobierz kolejne gałęzie</button>}
        <button type="button" className="secondary" disabled={busy} onClick={() => void readOverview()}>Sprawdź PR/MR i CI</button>
        {onSelect && <button type="button" disabled={busy || !branch.trim()} onClick={() => onSelect(repository, branch.trim(), profile!.credential_id)}>Użyj repozytorium i gałęzi</button>}
      </div>
    </>}
    {overview && <>
      {overview.warnings.map((warning, index) => <p className="error-banner" key={index}>{warning}</p>)}
      {[['PR / MR', overview.requests], ['CI / pipelines', overview.pipelines]].map(([title, items]) => <div key={title as string}><h3>{title as string}</h3><div className="table-scroll"><table><thead><tr><th>Nazwa</th><th>Gałąź</th><th>Status</th></tr></thead><tbody>{(items as Activity[]).map((item) => <tr key={item.id}><td>{item.url ? <a href={item.url} target="_blank" rel="noreferrer">{item.title}</a> : item.title}</td><td>{item.branch}</td><td>{item.status}</td></tr>)}{!(items as Activity[]).length && <tr><td colSpan={3}>Brak wyników; ewentualny błąd dostępu jest pokazany powyżej.</td></tr>}</tbody></table></div></div>)}
    </>}
    {!profiles.length && <p className="muted">Brak integracji. Administrator konfiguruje je w Danych dostępowych.</p>}
  </section>
}

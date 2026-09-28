import { useCallback, useEffect, useState } from 'react'
import { request } from '../api/client'
import type { GitCommit, GitState, Job, ProjectSourceControl, SourceControlCIStatus, SourceControlPageResult, SourceControlPullRequest } from '../api/types'
import { useAuth } from '../auth/AuthContext'

interface Props {
  projectId: string
}

interface CommitPage {
  items: GitCommit[]
  page: number
  per_page: number
  has_more: boolean
}

export function ProjectSourceControlSection({ projectId }: Props) {
  const { user } = useAuth()
  const [source, setSource] = useState<ProjectSourceControl | null>(null)
  const [git, setGit] = useState<GitState | null>(null)
  const [commits, setCommits] = useState<GitCommit[]>([])
  const [pulls, setPulls] = useState<SourceControlPullRequest[]>([])
  const [ci, setCI] = useState<SourceControlCIStatus | null>(null)
  const [tags, setTags] = useState<string[]>([])
  const [checkout, setCheckout] = useState('')
  const [commitPage, setCommitPage] = useState(1)
  const [hasMoreCommits, setHasMoreCommits] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const loadLocal = useCallback(async (page = 1) => {
    const [state, history, tagList] = await Promise.all([
      request<GitState>(`/projects/${encodeURIComponent(projectId)}/git`),
      request<CommitPage>(`/projects/${encodeURIComponent(projectId)}/git/commits?page=${page}&per_page=30`),
      request<string[]>(`/projects/${encodeURIComponent(projectId)}/git/tags`)
    ])
    setGit(state)
    setCheckout(state.branch)
    setCommits(history.items)
    setCommitPage(history.page)
    setHasMoreCommits(history.has_more)
    setTags(tagList)
  }, [projectId])

  const loadProvider = useCallback(async () => {
    try {
      const linked = await request<ProjectSourceControl>(`/projects/${encodeURIComponent(projectId)}/source-control`)
      setSource(linked)
      const [pullResult, ciResult] = await Promise.all([
        request<SourceControlPageResult<SourceControlPullRequest>>(`/projects/${encodeURIComponent(projectId)}/source-control/pull-requests?state=all&page=1&per_page=30`),
        request<SourceControlCIStatus>(`/projects/${encodeURIComponent(projectId)}/source-control/ci`)
      ])
      setPulls(pullResult.items)
      setCI(ciResult)
    } catch {
      setSource(null)
      setPulls([])
      setCI(null)
    }
  }, [projectId])

  useEffect(() => {
    setError('')
    void Promise.all([loadLocal(), loadProvider()]).catch((reason: unknown) =>
      setError(reason instanceof Error ? reason.message : 'Nie udało się pobrać danych Git')
    )
  }, [loadLocal, loadProvider])

  async function gitAction(action: 'fetch' | 'pull') {
    setBusy(action)
    setError('')
    try {
      await request<Job>(`/projects/${encodeURIComponent(projectId)}/git/${action}`, { method: 'POST', body: '{}' })
      await loadLocal()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : `Operacja ${action} nie powiodła się`)
    } finally {
      setBusy('')
    }
  }

  async function checkoutBranch() {
    if (!checkout) return
    setBusy('checkout')
    setError('')
    try {
      await request<Job>(`/projects/${encodeURIComponent(projectId)}/git/checkout`, {
        method: 'POST',
        body: JSON.stringify({ branch: checkout })
      })
      await loadLocal()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Checkout nie powiódł się')
    } finally {
      setBusy('')
    }
  }

  return <section className="source-control-section">
    <div className="section-heading">
      <div>
        <h2>Source Control</h2>
        <p className="muted">Lokalne operacje Git są oddzielone od API GitHub/GitLab. Checkout nie nadpisuje brudnego working tree.</p>
      </div>
      {user?.role !== 'viewer' && <div className="actions">
        <button type="button" className="secondary" disabled={Boolean(busy)} onClick={() => void gitAction('fetch')}>Fetch</button>
        <button type="button" className="secondary" disabled={Boolean(busy)} onClick={() => void gitAction('pull')}>Pull</button>
      </div>}
    </div>

    {error && <div className="error-banner">{error}</div>}

    {git && <div className="detail-grid">
      <div className="detail-field"><span>Branch</span><strong>{git.branch || '—'}</strong></div>
      <div className="detail-field"><span>Status</span><strong>{git.dirty ? 'Local changes' : git.behind > 0 ? `Behind ${git.behind}` : git.ahead > 0 ? `Ahead ${git.ahead}` : 'Up to date'}</strong></div>
      <div className="detail-field"><span>Ahead / Behind</span><strong>{git.ahead} / {git.behind}</strong></div>
      <div className="detail-field"><span>Commit</span><strong className="mono">{git.commit?.slice(0, 12) || '—'}</strong></div>
      <div className="detail-field"><span>Remote</span><strong>{git.remote || 'origin'}</strong></div>
      <div className="detail-field"><span>Tags</span><strong>{tags.length}</strong></div>
    </div>}

    {user?.role !== 'viewer' && git && <div className="panel source-checkout">
      <label>Checkout branch
        <input list="project-git-branches" value={checkout} onChange={(event) => setCheckout(event.target.value)} />
      </label>
      <datalist id="project-git-branches">{git.branches.map((branch) => <option value={branch} key={branch} />)}</datalist>
      <button type="button" disabled={Boolean(busy) || !checkout || checkout === git.branch} onClick={() => void checkoutBranch()}>Checkout</button>
      {git.dirty && <span className="warning-banner">Working tree contains local changes. Backend will reject checkout with HTTP 409.</span>}
    </div>}

    {source && <div className="panel">
      <div className="runtime-version-heading">
        <div>
          <h3>{source.provider === 'github' ? 'GitHub' : 'GitLab'}</h3>
          <p className="muted">{source.repository_path}</p>
        </div>
        <a className="button-link secondary" href={source.web_url} target="_blank" rel="noreferrer">Open Repository</a>
      </div>
      <div className="detail-grid">
        <div className="detail-field"><span>Repository</span><strong>{source.repository_path}</strong></div>
        <div className="detail-field"><span>Default branch</span><strong>{source.default_branch}</strong></div>
        <div className="detail-field"><span>Current branch</span><strong>{source.branch || git?.branch || '—'}</strong></div>
        <div className="detail-field"><span>Repository ID</span><strong>{source.repository_external_id}</strong></div>
      </div>
    </div>}

    <div className="split-panel source-control-split">
      <div className="panel">
        <div className="runtime-version-heading"><h3>Commits</h3><span className="muted">Page {commitPage}</span></div>
        <div className="table-scroll">
          <table>
            <thead><tr><th>SHA</th><th>Message</th><th>Author</th><th>Date</th></tr></thead>
            <tbody>
              {commits.map((commit) => <tr key={commit.hash}>
                <td>{source ? <a href={`${source.web_url}/${source.provider === 'gitlab' ? '-/commit' : 'commit'}/${commit.hash}`} target="_blank" rel="noreferrer" className="mono">{commit.hash.slice(0, 8)}</a> : <code>{commit.hash.slice(0, 8)}</code>}</td>
                <td>{commit.subject}</td><td>{commit.author}</td><td>{new Date(commit.date).toLocaleString()}</td>
              </tr>)}
              {commits.length === 0 && <tr><td colSpan={4} className="muted">Brak commitów.</td></tr>}
            </tbody>
          </table>
        </div>
        <div className="pagination-actions">
          <button type="button" className="secondary" disabled={commitPage <= 1} onClick={() => void loadLocal(commitPage - 1)}>Previous</button>
          <button type="button" className="secondary" disabled={!hasMoreCommits} onClick={() => void loadLocal(commitPage + 1)}>Next</button>
        </div>
      </div>

      <div className="stack">
        <div className="panel">
          <h3>CI</h3>
          {!ci && <p className="muted">Provider CI metadata unavailable.</p>}
          {ci && !ci.available && <p className="muted">{ci.message || 'CI information unavailable for current credentials.'}</p>}
          {ci?.available && <>
            <span className={`badge ${ci.status === 'success' ? 'badge-ok' : 'badge-warn'}`}>{ci.status}</span>
            <p><strong>{ci.name || 'Pipeline'}</strong>{ci.run_number ? ` #${ci.run_number}` : ''}</p>
            <p className="muted mono">{ci.commit_sha?.slice(0, 12)}</p>
            {ci.web_url && <a href={ci.web_url} target="_blank" rel="noreferrer">Open Actions / Pipelines</a>}
          </>}
        </div>

        <div className="panel">
          <h3>Pull / Merge Requests</h3>
          <div className="source-pr-list">
            {pulls.map((pull) => <a className="source-pr-item" href={pull.web_url} target="_blank" rel="noreferrer" key={pull.number}>
              <strong>#{pull.number} {pull.title}</strong>
              <span>{pull.state} · {pull.author}</span>
              <small>{pull.source_branch} → {pull.target_branch} · {new Date(pull.updated_at).toLocaleString()}</small>
            </a>)}
            {pulls.length === 0 && <p className="muted">Brak PR/MR lub integracja providera nie jest przypisana.</p>}
          </div>
        </div>
      </div>
    </div>
  </section>
}

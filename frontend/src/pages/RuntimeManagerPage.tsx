import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { Job } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { DurableJobNotice, readPendingJob, storePendingJob } from '../components/DurableJobNotice'

interface Version { runtime: string; version: string; reference: string; installed: boolean; id: string; size?: string; digest?: string; projects: {id: string; name: string}[] }
const pendingKey = 'devbox.runtime-image-job'
export function RuntimeManagerPage() {
  const { user } = useAuth()
  const [runtime, setRuntime] = useState('php')
  const [installed, setInstalled] = useState<Version[]>([])
  const [available, setAvailable] = useState<string[]>([])
  const [version, setVersion] = useState('')
  const [jobId, setJobId] = useState(() => readPendingJob(pendingKey))
  const [pending, setPending] = useState(() => Boolean(readPendingJob(pendingKey)))
  const [error, setError] = useState('')
  const [catalogError, setCatalogError] = useState('')
  const [loading, setLoading] = useState(false)
  const load = useCallback(async () => setInstalled(await request<Version[]>(`/runtime-images/${runtime}`)), [runtime])
  useEffect(() => {
    const controller = new AbortController()
    setError(''); setCatalogError(''); setInstalled([]); setAvailable([]); setVersion(''); setLoading(true)
    void request<Version[]>(`/runtime-images/${runtime}`, { signal: controller.signal }).then(setInstalled).catch((cause: unknown) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Odczyt lokalnych obrazów nie powiódł się') })
    void request<string[]>(`/runtime-images/${runtime}/versions`, { signal: controller.signal }).then(setAvailable).catch((cause: unknown) => { if (!controller.signal.aborted) setCatalogError(cause instanceof Error ? cause.message : 'Katalog wersji jest niedostępny') }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [runtime])
  async function mutate(action: 'pull' | 'remove', selected = version) {
    if (action === 'remove' && !window.confirm(`Usunąć lokalny obraz ${runtime}:${selected}?`)) return
    setError(''); setPending(true)
    try {
      const job = await request<Job>(`/runtime-images/${runtime}/actions`, { method: 'POST', body: JSON.stringify({ action, version: selected }) })
      storePendingJob(pendingKey, job.id); setJobId(job.id)
    } catch (cause) { setPending(false); setError(cause instanceof Error ? cause.message : 'Operacja nie powiodła się') }
  }
  function finish(job: Job) {
    storePendingJob(pendingKey, ''); setPending(false)
    if (job.status !== 'succeeded') setError(job.error || job.status)
    void load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Odczyt statusu nie powiódł się'))
  }
  return <>
    <h1>Runtime Manager</h1>
    <p className="muted">Obrazy kontenerów używane przez generowane runtime. Instalacja nie zmienia PHP, Node.js, Pythona ani Go na hoście.</p>
    {error && <div className="error-banner">{error}</div>}
    {jobId && <DurableJobNotice jobId={jobId} onComplete={finish} />}
    <section className="panel">
      <div className="form-grid">
        <label>Runtime<select value={runtime} onChange={(e) => setRuntime(e.target.value)}>{[['php', 'PHP'], ['node', 'Node.js'], ['python', 'Python'], ['go', 'Go'], ['static', 'Static / Nginx']].map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
        <label>Wersja obrazu<input list="runtime-available-versions" value={version} onChange={(e) => setVersion(e.target.value)} placeholder="Wybierz wersję lub wpisz ręcznie" /><datalist id="runtime-available-versions">{available.map((v) => <option key={v} value={v} />)}</datalist></label>
      </div>
      <p className="muted">{loading ? 'Pobieranie katalogu z rejestru obrazów…' : `Wersje w katalogu: ${available.length}.`}</p>
      {catalogError && <p className="error-banner">{catalogError}. Wersję można podać ręcznie; istnienie obrazu zostanie zweryfikowane przy pobieraniu.</p>}
      {user?.role !== 'viewer' && <button type="button" disabled={pending || !version.trim()} onClick={() => void mutate('pull')}>Pobierz obraz</button>}
    </section>
    <section className="panel">
      <h2>Obrazy lokalne</h2>
      <div className="table-scroll"><table><thead><tr><th>Wersja / obraz</th><th>Rozmiar</th><th>Używające projekty</th><th>Akcje</th></tr></thead><tbody>
        {installed.map((item) => <tr key={item.reference}><td><strong>{item.version}</strong><br /><code>{item.reference}</code><br /><small>{item.digest || item.id}</small></td><td>{item.size || '—'}</td><td>{item.projects.length ? item.projects.map((p) => <div key={p.id}><Link to={`/apps/${encodeURIComponent(p.id)}?tab=runtime`}>{p.name}</Link></div>) : 'Brak przypisań'}</td><td>{user?.role !== 'viewer' && <button type="button" className="danger" disabled={pending || item.projects.length > 0} onClick={() => void mutate('remove', item.version)}>Usuń nieużywany obraz</button>}</td></tr>)}
        {!installed.length && <tr><td colSpan={4}>Brak lokalnych obrazów tego runtime lub odczyt jest jeszcze w toku.</td></tr>}
      </tbody></table></div>
      <p className="muted small">Nie można usunąć wersji przypisanej do projektu. Docker dodatkowo blokuje usuwanie obrazów używanych przez kontenery; DevBox nie używa wymuszonego usuwania.</p>
    </section>
  </>
}

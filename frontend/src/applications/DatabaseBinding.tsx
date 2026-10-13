import { useCallback, useState } from 'react'
import type { ApplicationDatabaseBinding, Job } from '../api/types'
import { request } from '../api/client'
import { usePolling } from '../control-room/usePolling'
import { message } from './model'
import { applyDatabaseChoice, DatabaseConfiguration, emptyDatabaseChoice } from './databaseConfiguration'
import { Link } from 'react-router-dom'
import { Icon } from '../components/Icon'

export function DatabaseBinding({ applicationId, disabled }: { applicationId: string; disabled: boolean }) {
  const load = useCallback((signal: AbortSignal) => request<{ binding: ApplicationDatabaseBinding; configured: boolean }>(`/applications/${encodeURIComponent(applicationId)}/database-binding`, { signal }), [applicationId])
  const state = usePolling(load, 10000)
  const [choice, setChoice] = useState(emptyDatabaseChoice)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [progress, setProgress] = useState('')
  const [jobID, setJobID] = useState('')
  async function update(action: 'save' | 'detach' | 'test') {
    setBusy(true); setError('')
    const base = `/applications/${encodeURIComponent(applicationId)}/database-binding`
    try {
      if (action === 'save') { await applyDatabaseChoice(applicationId, choice, setProgress); setProgress('Zapisano. Wdróż aplikację, aby zastosować nowe dane połączenia.'); state.refresh() }
      if (action === 'detach') { await request(base, { method: 'DELETE' }); state.refresh(); setProgress('Odłączono. Wdróż aplikację, aby usunąć zmienne bazy z kontenera.') }
      if (action === 'test') { const job = await request<Job>(`${base}/test`, { method: 'POST' }); setJobID(job.id) }
    } catch (cause) { setError(message(cause)) } finally { setBusy(false) }
  }
  const binding = state.data?.binding
  return <section className="acp-card"><div className="app-section-title"><Icon name="database" /><h2>Baza danych aplikacji</h2></div>
    {(error || state.error) && <p className="error-banner" role="alert">{error || state.error}</p>}
    {progress && <p role="status">{progress}</p>}
    {state.data && (binding?.database_id ? <p className="app-database-status"><Icon name="check" size={17} />Podłączono: <strong>{binding.database_name}</strong><span>{binding.engine} · konto {binding.username}</span></p> : <p className="app-database-status"><Icon name="database" size={17} />Brak przypisanej bazy.</p>)}
    {!state.data && !state.error && <p className="muted">Wczytywanie połączenia…</p>}
    <fieldset disabled={disabled || busy}><legend>Konfiguracja połączenia</legend><DatabaseConfiguration value={choice} onChange={setChoice} /><div className="acp-actions app-form-actions"><button disabled={choice.mode === 'none'} onClick={() => void update('save')}>Zapisz połączenie</button>{binding?.database_id && <><button onClick={() => void update('test')}>Test SQL z aplikacji</button><button className="secondary-button" onClick={() => void update('detach')}>Odłącz bazę</button></>}</div></fieldset>
    {jobID && <Link to={`/jobs?job=${encodeURIComponent(jobID)}`}>Wynik testu uwierzytelnienia i SELECT 1</Link>}
  </section>
}

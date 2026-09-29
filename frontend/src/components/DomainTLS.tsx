import { useEffect, useState } from 'react'
import { request } from '../api/client'
import type { Job } from '../api/types'
import { DurableJobNotice, readPendingJob, storePendingJob } from './DurableJobNotice'

interface TLSStatus {
  enabled: boolean; hostname: string; expires?: string; verified_at?: string
  fingerprint?: string; ca_fingerprint?: string
}
export function DomainTLS({ id, admin, onChanged }: { id: string; admin: boolean; onChanged: () => void }) {
  const storageKey = `devbox.tls.${id}`
  const [status, setStatus] = useState<TLSStatus | null>(null)
  const [error, setError] = useState('')
  const [jobId, setJobId] = useState(() => readPendingJob(storageKey))
  const [busy, setBusy] = useState(!!jobId)
  async function refresh() {
    try { setStatus(await request<TLSStatus>(`/domains/${encodeURIComponent(id)}/tls`)); setError('') }
    catch (e) { setError(e instanceof Error ? e.message : 'Błąd statusu HTTPS') }
  }
  useEffect(() => { void refresh() }, [id])
  async function act(action: string) {
    if (action === 'disable' && !window.confirm('Wyłączyć HTTPS i przekierowanie HTTP dla tej domeny?')) return
    setError(''); setBusy(true)
    try {
      const job = await request<Job>(`/domains/${encodeURIComponent(id)}/tls`, { method: 'POST', body: JSON.stringify({ action }) })
      setJobId(job.id); storePendingJob(storageKey, job.id)
    } catch (e) { setBusy(false); setError(e instanceof Error ? e.message : 'Nie udało się uruchomić operacji TLS') }
  }
  return <details>
    <summary>HTTPS: {status ? status.enabled ? 'włączone' : 'wyłączone' : 'odczyt…'}</summary>
    {error && <p role="alert" className="error-banner">{error}</p>}
    {status?.enabled && <>
      <p><a href={`https://${status.hostname}/`} target="_blank" rel="noopener noreferrer">Otwórz HTTPS</a></p>
      <p>Ważność: {status.expires ? new Date(status.expires).toLocaleString() : 'brak danych'}</p>
      <p>Ostatnia weryfikacja serwera: {status.verified_at ? new Date(status.verified_at).toLocaleString() : 'niezweryfikowany'}</p>
      <p className="muted">Port TLS: 443 (Nginx). Port aplikacji pozostaje HTTP. Weryfikacja serwera nie oznacza zaufania przeglądarki.</p>
    </>}
    {status?.ca_fingerprint && <>
      <a href="/api/v1/proxy/tls/ca.crt">Pobierz publiczny certyfikat lokalnego CA</a>
      <p className="muted">Zainstaluj ten certyfikat jako zaufany tylko na przeznaczonych do tego urządzeniach deweloperskich. W Windows/WSL zaufanie Windows i Linux jest oddzielne. DevBox nie instaluje CA na urządzeniu klienta.</p>
      <p style={{ overflowWrap: 'anywhere' }}>SHA-256 CA: <code>{status.ca_fingerprint}</code></p>
    </>}
    {!status?.enabled && <p className="muted">Lokalny certyfikat dla localhost, prywatnych IP i domen .localhost / .test / .local / .internal / .lan. Domeny publiczne wymagają osobnej konfiguracji certyfikatu.</p>}
    {admin && <div className="row-actions">
      <button type="button" className="secondary" disabled={busy || !status} onClick={() => void act(status?.enabled ? 'renew' : 'enable')}>{status?.enabled ? 'Odnów certyfikat' : 'Włącz lokalne HTTPS'}</button>
      {status?.enabled && <>
        <button type="button" className="secondary" disabled={busy} onClick={() => void act('verify')}>Zweryfikuj certyfikat serwera</button>
        <button type="button" className="danger" disabled={busy} onClick={() => void act('disable')}>Wyłącz HTTPS</button>
      </>}
    </div>}
    {jobId && <DurableJobNotice jobId={jobId} onComplete={() => { setBusy(false); storePendingJob(storageKey, ''); void refresh(); onChanged() }} />}
  </details>
}

import { useEffect, useRef, useState } from 'react'
import { apiURL } from '../api/client'
import {
  createSystemBackup,
  deleteSystemBackup,
  importSystemBackup,
  listSystemBackups,
  restoreSystemBackup
} from '../api/operations'
import type { SystemBackup } from '../api/types'
import { ErrorState } from '../components/ErrorState'

function formatSize(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

export function BackupsPage() {
  const [items, setItems] = useState<SystemBackup[]>([])
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  async function reload() {
    try {
      setItems(await listSystemBackups())
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }

  useEffect(() => {
    void reload()
    const timer = window.setInterval(() => void reload(), 3000)
    return () => window.clearInterval(timer)
  }, [])

  async function create() {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      await createSystemBackup()
      setMessage('Backup został dodany do kolejki.')
      await reload()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  async function importFile(file?: File) {
    if (!file) return
    setBusy(true)
    setError('')
    setMessage('')
    try {
      await importSystemBackup(file)
      setMessage('Import backupu został dodany do kolejki walidacji.')
      await reload()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  async function restore(item: SystemBackup) {
    if (!window.confirm(`Przygotować restore backupu ${item.file_name}? Zostanie zastosowany przy następnym restarcie DevBox.`)) return
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const result = await restoreSystemBackup(item.id)
      setMessage(result.message)
      await reload()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  async function remove(item: SystemBackup) {
    if (!window.confirm(`Usunąć backup ${item.file_name}?`)) return
    setBusy(true)
    setError('')
    try {
      await deleteSystemBackup(item.id)
      await reload()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  async function download(item: SystemBackup) {
    setError('')
    try {
      const response = await fetch(apiURL(`/system-backups/${encodeURIComponent(item.id)}/download`), { credentials: 'include' })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const blob = await response.blob()
      const href = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = href
      anchor.download = item.file_name
      anchor.click()
      URL.revokeObjectURL(href)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }

  return <>
    <div className="page-heading">
      <div>
        <h1>Backup DevBox</h1>
        <p className="muted">Snapshot SQLite, zaszyfrowane sekrety, konfiguracja Nginx oraz kontrolowane pliki Docker/Compose. Master key nie jest zapisywany w backupie.</p>
      </div>
      <div className="button-row">
        <button type="button" disabled={busy} onClick={() => void create()}>Utwórz backup</button>
        <button type="button" className="secondary" disabled={busy} onClick={() => fileRef.current?.click()}>Importuj backup</button>
        <input ref={fileRef} type="file" accept=".gz,.tgz,application/gzip" hidden onChange={event => void importFile(event.target.files?.[0])} />
      </div>
    </div>

    {error && <ErrorState message={error} />}
    {message && <div className="panel"><strong>{message}</strong></div>}

    <div className="panel">
      <div className="table-scroll"><table>
        <thead><tr><th>Plik</th><th>Status</th><th>Rozmiar</th><th>SHA-256</th><th>Utworzono</th><th>Akcje</th></tr></thead>
        <tbody>
          {items.map(item => <tr key={item.id}>
            <td><code>{item.file_name}</code>{item.error && <div className="muted">{item.error}</div>}</td>
            <td>{item.status}</td>
            <td>{formatSize(item.size_bytes)}</td>
            <td><code title={item.sha256}>{item.sha256 ? item.sha256.slice(0, 16) + '…' : '—'}</code></td>
            <td>{new Date(item.created_at).toLocaleString()}</td>
            <td>
              <div className="button-row">
                {(item.status === 'ready' || item.status === 'pending_restore') && <button type="button" className="secondary" onClick={() => void download(item)}>Pobierz</button>}
                {item.status === 'ready' && <button type="button" disabled={busy} onClick={() => void restore(item)}>Restore</button>}
                {!['running','importing','restoring','pending_restore'].includes(item.status) && <button type="button" className="secondary" disabled={busy} onClick={() => void remove(item)}>Usuń</button>}
              </div>
            </td>
          </tr>)}
          {items.length === 0 && <tr><td colSpan={6} className="muted">Brak backupów.</td></tr>}
        </tbody>
      </table></div>
    </div>
  </>
}

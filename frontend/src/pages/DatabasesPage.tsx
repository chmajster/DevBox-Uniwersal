import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { DatabaseBackup, DatabaseRecord, DatabaseUser, Job, MySQLStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'

function formatBytes(value?: number) {
  if (value === undefined || value === null) return '—'
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let current = value / 1024
  let unit = 0
  while (current >= 1024 && unit < units.length - 1) {
    current /= 1024
    unit += 1
  }
  return `${current.toFixed(current >= 10 ? 1 : 2)} ${units[unit]}`
}

export function DatabasesPage() {
  const { user } = useAuth()
  const canMutate = user?.role !== 'viewer'
  const [databases, setDatabases] = useState<DatabaseRecord[]>([])
  const [users, setUsers] = useState<DatabaseUser[]>([])
  const [mysql, setMysql] = useState<MySQLStatus | null>(null)
  const [selectedDatabase, setSelectedDatabase] = useState<DatabaseRecord | null>(null)
  const [backups, setBackups] = useState<DatabaseBackup[]>([])
  const [databaseName, setDatabaseName] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const userCountByDatabase = useMemo(() => {
    const counts = new Map<string, number>()
    users.forEach((item) => counts.set(item.database_id, (counts.get(item.database_id) ?? 0) + 1))
    return counts
  }, [users])

  const load = useCallback(async () => {
    const [databaseItems, databaseUsers, mysqlStatus] = await Promise.all([
      request<DatabaseRecord[]>('/databases'),
      request<DatabaseUser[]>('/database-users'),
      request<MySQLStatus>('/mysql/status'),
    ])
    setDatabases(databaseItems ?? [])
    setUsers(databaseUsers ?? [])
    setMysql(mysqlStatus)
  }, [])

  useEffect(() => {
    load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać danych baz'))
  }, [load])

  async function run(action: () => Promise<void>) {
    setBusy(true); setError(''); setMessage('')
    try { await action() }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Operacja nie powiodła się') }
    finally { setBusy(false) }
  }

  async function createDatabase(event: FormEvent) {
    event.preventDefault()
    await run(async () => {
      const item = await request<DatabaseRecord>('/databases', {
        method: 'POST',
        body: JSON.stringify({ name: databaseName, engine: 'mysql', charset: 'utf8mb4' }),
      })
      setDatabaseName('')
      setMessage(`Baza ${item.name} została utworzona.`)
      await load()
    })
  }

  async function removeDatabase(item: DatabaseRecord) {
    if (!window.confirm(`Usunąć bazę ${item.name}? Zostaną usunięci również zarządzani użytkownicy tej bazy.`)) return
    await run(async () => {
      await request<{ status: string }>(`/databases/${item.id}`, { method: 'DELETE' })
      if (selectedDatabase?.id === item.id) {
        setSelectedDatabase(null)
        setBackups([])
      }
      await load()
    })
  }

  async function queueBackup(item: DatabaseRecord) {
    await run(async () => {
      const result = await request<{ job: Job; backup: DatabaseBackup }>(`/databases/${item.id}/backup`, { method: 'POST' })
      setMessage(`Backup dodany do kolejki jako zadanie ${result.job.id}.`)
      if (selectedDatabase?.id === item.id) await showBackups(item)
    })
  }

  async function showBackups(item: DatabaseRecord) {
    const items = await request<DatabaseBackup[]>(`/databases/${item.id}/backups`)
    setSelectedDatabase(item)
    setBackups(items ?? [])
  }

  async function restoreBackup(backup: DatabaseBackup) {
    if (!selectedDatabase) return
    if (!window.confirm(`Przywrócić ${backup.file_name} do ${selectedDatabase.name}?`)) return
    await run(async () => {
      const job = await request<Job>(`/databases/${selectedDatabase.id}/restore`, {
        method: 'POST',
        body: JSON.stringify({ backup_id: backup.id }),
      })
      setMessage(`Restore dodany do kolejki jako zadanie ${job.id}.`)
    })
  }

  async function deleteBackup(backup: DatabaseBackup) {
    await run(async () => {
      await request<{ status: string }>(`/database-backups/${backup.id}`, { method: 'DELETE' })
      if (selectedDatabase) await showBackups(selectedDatabase)
    })
  }

  return <>
    <div className="page-heading">
      <div><h1>Bazy danych</h1><p className="muted">MySQL/MariaDB provisioning, scoped grants i backupy.</p></div>
      <div className="actions">
        <Link className="button-link secondary" to="/database-users">Użytkownicy baz</Link>
        <div className="status-chip" data-ok={mysql?.running ? 'true' : 'false'}>MySQL: {mysql?.running ? mysql.version || 'connected' : 'unavailable'}</div>
      </div>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}

    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Adres MySQL/MariaDB</h2>
          <p className="muted">Używaj adresu aplikacyjnego w kontenerach i projektach DevBox. Adres administracyjny jest przeznaczony dla control-plane DevBox.</p>
        </div>
        <span className="status-chip" data-ok={mysql?.running ? 'true' : 'false'}>{mysql?.running ? 'Dostępny' : 'Niedostępny'}</span>
      </div>
      <div className="summary-grid">
        <div><span>Adres dla aplikacji</span><strong><code>{mysql?.application_host && mysql?.application_port ? `${mysql.application_host}:${mysql.application_port}` : '—'}</code></strong></div>
        <div><span>Host</span><strong><code>{mysql?.application_host || '—'}</code></strong></div>
        <div><span>Port</span><strong>{mysql?.application_port ?? '—'}</strong></div>
        <div><span>Sieć Docker</span><strong><code>{mysql?.network || '—'}</code></strong></div>
        <div><span>Adres administracyjny</span><strong><code>{mysql?.admin_host && mysql?.admin_port ? `${mysql.admin_host}:${mysql.admin_port}` : '—'}</code></strong></div>
        <div><span>Zmienna aplikacji</span><strong><code>{mysql?.application_host ? `DB_HOST=${mysql.application_host}` : '—'}</code></strong></div>
      </div>
    </section>

    {canMutate && <form className="panel compact-form" onSubmit={createDatabase}>
      <h2>Utwórz bazę</h2>
      <label>Nazwa<input value={databaseName} onChange={(event) => setDatabaseName(event.target.value)} placeholder="app_db" required /></label>
      <button type="submit" disabled={busy}>Utwórz bazę</button>
    </form>}

    <section className="panel">
      <div className="table-scroll">
        <table>
          <thead><tr><th>Baza</th><th>Aplikacja</th><th>Użytkownicy</th><th>Rozmiar</th><th>Utworzono</th><th>Status</th><th>Akcje</th></tr></thead>
          <tbody>
            {databases.map((item) => <tr key={item.id}>
              <td><strong>{item.name}</strong><div className="muted small">{item.engine}</div></td>
              <td>{item.application_name || '—'}</td>
              <td><span className="badge badge-muted">{userCountByDatabase.get(item.id) ?? 0}</span></td>
              <td>{formatBytes(item.size_bytes)}</td>
              <td>{new Date(item.created_at).toLocaleString()}</td>
              <td>{item.status}</td>
              <td className="actions">
                <Link className="button-link secondary button-compact" to={`/database-users?database=${encodeURIComponent(item.id)}`}>Użytkownicy</Link>
                <button type="button" className="secondary" onClick={() => run(() => showBackups(item))}>Backupy</button>
                {canMutate && <button type="button" className="secondary" onClick={() => queueBackup(item)} disabled={busy}>Backup</button>}
                {canMutate && <button type="button" className="danger" onClick={() => removeDatabase(item)} disabled={busy}>Usuń</button>}
              </td>
            </tr>)}
            {databases.length === 0 && <tr><td colSpan={7} className="muted">Brak zarządzanych baz.</td></tr>}
          </tbody>
        </table>
      </div>
    </section>

    {selectedDatabase && <section className="panel backup-panel">
      <div className="page-heading">
        <div><h2>Backupy: {selectedDatabase.name}</h2><p className="muted">Backup i restore są wykonywane jako Jobs.</p></div>
        <button type="button" className="secondary" onClick={() => run(() => showBackups(selectedDatabase))}>Odśwież</button>
      </div>
      <div className="table-scroll">
        <table>
          <thead><tr><th>Plik</th><th>Status</th><th>Rozmiar</th><th>Utworzono</th><th>Akcje</th></tr></thead>
          <tbody>
            {backups.map((backup) => <tr key={backup.id}>
              <td>{backup.file_name}</td><td>{backup.status}</td><td>{formatBytes(backup.size_bytes)}</td><td>{new Date(backup.created_at).toLocaleString()}</td>
              <td className="actions">
                {backup.status === 'ready' && <button type="button" className="secondary" onClick={() => window.open(`/api/v1/database-backups/${backup.id}/download`, '_blank', 'noopener,noreferrer')}>Pobierz</button>}
                {canMutate && backup.status === 'ready' && <button type="button" className="secondary" onClick={() => restoreBackup(backup)} disabled={busy}>Restore</button>}
                {canMutate && <button type="button" className="danger" onClick={() => deleteBackup(backup)} disabled={busy}>Usuń</button>}
              </td>
            </tr>)}
            {backups.length === 0 && <tr><td colSpan={5} className="muted">Brak backupów.</td></tr>}
          </tbody>
        </table>
      </div>
    </section>}
  </>
}

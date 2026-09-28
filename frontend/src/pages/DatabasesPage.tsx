import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { request } from '../api/client'
import type { DatabaseBackup, DatabaseRecord, Job, MySQLStatus, PHPMyAdminStatus } from '../api/types'
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
  const [mysql, setMysql] = useState<MySQLStatus | null>(null)
  const [phpMyAdmin, setPHPMyAdmin] = useState<PHPMyAdminStatus | null>(null)
  const [selectedDatabase, setSelectedDatabase] = useState<DatabaseRecord | null>(null)
  const [backups, setBackups] = useState<DatabaseBackup[]>([])
  const [databaseName, setDatabaseName] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [phpMyAdminInstalling, setPHPMyAdminInstalling] = useState(false)
  const [phpMyAdminInstallProgress, setPHPMyAdminInstallProgress] = useState<number | null>(null)
  const [phpMyAdminInstallPhase, setPHPMyAdminInstallPhase] = useState('')

  const load = useCallback(async () => {
    const [databaseItems, mysqlStatus, phpStatus] = await Promise.all([
      request<DatabaseRecord[]>('/databases'),
      request<MySQLStatus>('/mysql/status'),
      request<PHPMyAdminStatus>('/phpmyadmin/status'),
    ])
    setDatabases(databaseItems ?? [])
    setMysql(mysqlStatus)
    setPHPMyAdmin(phpStatus)
  }, [])

  useEffect(() => {
    load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Failed to load databases'))
  }, [load])

  async function run(action: () => Promise<void>) {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      await action()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Operation failed')
    } finally {
      setBusy(false)
    }
  }

  async function createDatabase(event: FormEvent) {
    event.preventDefault()
    await run(async () => {
      const item = await request<DatabaseRecord>('/databases', {
        method: 'POST',
        body: JSON.stringify({ name: databaseName, engine: 'mysql', charset: 'utf8mb4' }),
      })
      setDatabaseName('')
      setMessage(`Database ${item.name} created.`)
      await load()
    })
  }

  async function removeDatabase(item: DatabaseRecord) {
    if (!window.confirm(`Delete database ${item.name}? This removes the MySQL database and managed users.`)) return
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
      setMessage(`Backup queued as job ${result.job.id}.`)
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
    if (!window.confirm(`Restore ${backup.file_name} into ${selectedDatabase.name}?`)) return
    await run(async () => {
      const job = await request<Job>(`/databases/${selectedDatabase.id}/restore`, {
        method: 'POST',
        body: JSON.stringify({ backup_id: backup.id }),
      })
      setMessage(`Restore queued as job ${job.id}.`)
    })
  }

  async function deleteBackup(backup: DatabaseBackup) {
    await run(async () => {
      await request<{ status: string }>(`/database-backups/${backup.id}`, { method: 'DELETE' })
      if (selectedDatabase) await showBackups(selectedDatabase)
    })
  }

  async function phpAction(action: 'install' | 'start' | 'stop' | 'restart') {
    let installTimer: number | undefined
    let installSucceeded = false

    if (action === 'install') {
      setPHPMyAdminInstalling(true)
      setPHPMyAdminInstallProgress(5)
      setPHPMyAdminInstallPhase('Przygotowywanie instalacji…')

      installTimer = window.setInterval(() => {
        setPHPMyAdminInstallProgress((current) => {
          const next = Math.min((current ?? 5) + 7, 92)
          if (next < 35) setPHPMyAdminInstallPhase('Przygotowywanie kontenera phpMyAdmin…')
          else if (next < 75) setPHPMyAdminInstallPhase('Pobieranie obrazu i uruchamianie kontenera…')
          else setPHPMyAdminInstallPhase('Weryfikacja stanu usługi…')
          return next
        })
      }, 700)
    }

    await run(async () => {
      const status = await request<PHPMyAdminStatus>(`/phpmyadmin/${action}`, { method: 'POST' })
      setPHPMyAdmin(status)
      if (action === 'install') installSucceeded = true
    })

    if (installTimer !== undefined) window.clearInterval(installTimer)

    if (action === 'install') {
      if (installSucceeded) {
        setPHPMyAdminInstallProgress(100)
        setPHPMyAdminInstallPhase('phpMyAdmin został zainstalowany.')
        window.setTimeout(() => {
          setPHPMyAdminInstalling(false)
          setPHPMyAdminInstallProgress(null)
          setPHPMyAdminInstallPhase('')
        }, 1200)
      } else {
        setPHPMyAdminInstalling(false)
        setPHPMyAdminInstallProgress(null)
        setPHPMyAdminInstallPhase('')
      }
    }
  }

  return <>
    <div className="page-heading">
      <div>
        <h1>Bazy danych</h1>
        <p className="muted">MySQL/MariaDB provisioning, users, scoped grants and backups.</p>
      </div>
      <div className="status-chip" data-ok={mysql?.running ? 'true' : 'false'}>
        MySQL: {mysql?.running ? mysql.version || 'connected' : 'unavailable'}
      </div>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}

    {canMutate && <form className="panel compact-form" onSubmit={createDatabase}>
      <h2>Utwórz bazę</h2>
      <label>Nazwa
        <input value={databaseName} onChange={(event) => setDatabaseName(event.target.value)} placeholder="app_db" required />
      </label>
      <button type="submit" disabled={busy}>Utwórz</button>
    </form>}

    <div className="table-scroll">
      <table>
        <thead><tr><th>Database</th><th>Application</th><th>User</th><th>Size</th><th>Created</th><th>Status</th><th>Actions</th></tr></thead>
        <tbody>
          {databases.map((item) => <tr key={item.id}>
            <td><strong>{item.name}</strong><div className="muted small">{item.engine}</div></td>
            <td>{item.application_name || '—'}</td>
            <td>{item.user || '—'}</td>
            <td>{formatBytes(item.size_bytes)}</td>
            <td>{new Date(item.created_at).toLocaleString()}</td>
            <td>{item.status}</td>
            <td className="actions">
              <button type="button" className="secondary" onClick={() => run(() => showBackups(item))}>Backupy</button>
              {canMutate && <button type="button" className="secondary" onClick={() => queueBackup(item)} disabled={busy}>Backup</button>}
              {canMutate && <button type="button" className="danger" onClick={() => removeDatabase(item)} disabled={busy}>Usuń</button>}
            </td>
          </tr>)}
          {databases.length === 0 && <tr><td colSpan={7} className="muted">Brak zarządzanych baz.</td></tr>}
        </tbody>
      </table>
    </div>

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
              <td>{backup.file_name}</td>
              <td>{backup.status}</td>
              <td>{formatBytes(backup.size_bytes)}</td>
              <td>{new Date(backup.created_at).toLocaleString()}</td>
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

    <section className="panel phpmyadmin-panel">
      <div>
        <h2>phpMyAdmin</h2>
        <p className="muted">Niezależny kontener Docker. Lifecycle aplikacji nie steruje phpMyAdmin.</p>
      </div>
      <div className="phpmyadmin-status">
        {phpMyAdminInstalling && phpMyAdminInstallProgress !== null && <div className="phpmyadmin-install-progress" role="status" aria-live="polite">
          <div className="phpmyadmin-install-progress-header">
            <div>
              <strong>{phpMyAdminInstallProgress < 100 ? 'Instalowanie phpMyAdmin' : 'Instalacja zakończona'}</strong>
              <span>{phpMyAdminInstallPhase}</span>
            </div>
            <strong className="phpmyadmin-install-percent">{phpMyAdminInstallProgress}%</strong>
          </div>
          <div
            className="phpmyadmin-progress-track"
            role="progressbar"
            aria-label="Postęp instalacji phpMyAdmin"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={phpMyAdminInstallProgress}
          >
            <div className="phpmyadmin-progress-value" style={{ width: `${phpMyAdminInstallProgress}%` }} />
          </div>
        </div>}
        <span className="status-chip" data-ok={phpMyAdmin?.running ? 'true' : 'false'}>{phpMyAdminInstalling ? 'installing' : (phpMyAdmin?.state ?? 'unknown')}</span>
        <div className="actions">
          {canMutate && !phpMyAdmin?.installed && <button type="button" onClick={() => phpAction('install')} disabled={busy || phpMyAdminInstalling}>{phpMyAdminInstalling ? 'Instalowanie…' : 'Install'}</button>}
          {canMutate && phpMyAdmin?.installed && !phpMyAdmin.running && <button type="button" onClick={() => phpAction('start')} disabled={busy}>Start</button>}
          {canMutate && phpMyAdmin?.running && <button type="button" className="secondary" onClick={() => phpAction('restart')} disabled={busy}>Restart</button>}
          {canMutate && phpMyAdmin?.running && <button type="button" className="secondary" onClick={() => phpAction('stop')} disabled={busy}>Stop</button>}
          {phpMyAdmin?.running && <button type="button" onClick={() => window.open(phpMyAdmin.url, '_blank', 'noopener,noreferrer')}>Open URL</button>}
        </div>
      </div>
    </section>
  </>
}

import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { request } from '../api/client'
import type { DatabaseBackup, DatabaseRecord, DatabaseUser, DatabaseUserCreateResult, Job, MySQLStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'

const AVAILABLE_PRIVILEGES = [
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'INDEX', 'ALTER',
  'REFERENCES', 'CREATE TEMPORARY TABLES', 'LOCK TABLES', 'EXECUTE',
  'CREATE VIEW', 'SHOW VIEW', 'TRIGGER',
]

const DEFAULT_PRIVILEGES = [
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'INDEX', 'ALTER',
  'REFERENCES', 'CREATE TEMPORARY TABLES', 'LOCK TABLES',
  'CREATE VIEW', 'SHOW VIEW', 'TRIGGER',
]

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
  const [newUserDatabaseId, setNewUserDatabaseId] = useState('')
  const [newUsername, setNewUsername] = useState('')
  const [newUserPrivileges, setNewUserPrivileges] = useState<string[]>(DEFAULT_PRIVILEGES)
  const [createdCredential, setCreatedCredential] = useState<{ username: string; password: string; database: string } | null>(null)
  const [editingUser, setEditingUser] = useState<DatabaseUser | null>(null)
  const [editingPrivileges, setEditingPrivileges] = useState<string[]>([])
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const databaseById = useMemo(() => new Map(databases.map((item) => [item.id, item])), [databases])
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
    setNewUserDatabaseId((current) => current || databaseItems?.[0]?.id || '')
  }, [])

  useEffect(() => {
    load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać danych baz'))
  }, [load])

  async function run(action: () => Promise<void>) {
    setBusy(true)
    setError('')
    setMessage('')
    try {
      await action()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Operacja nie powiodła się')
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
      setMessage(`Baza ${item.name} została utworzona.`)
      await load()
    })
  }

  async function createDatabaseUser(event: FormEvent) {
    event.preventDefault()
    if (!newUserDatabaseId) return
    await run(async () => {
      const result = await request<DatabaseUserCreateResult>('/database-users', {
        method: 'POST',
        body: JSON.stringify({
          database_id: newUserDatabaseId,
          username: newUsername.trim(),
          privileges: newUserPrivileges,
        }),
      })
      const database = databaseById.get(newUserDatabaseId)
      setCreatedCredential({
        username: result.credential.username,
        password: result.credential.password,
        database: database?.name ?? newUserDatabaseId,
      })
      setNewUsername('')
      setNewUserPrivileges(DEFAULT_PRIVILEGES)
      setMessage(`Użytkownik ${result.user.username} został przypisany do bazy ${database?.name ?? ''}.`)
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

  async function removeUser(item: DatabaseUser) {
    const database = databaseById.get(item.database_id)
    if (!window.confirm(`Usunąć użytkownika ${item.username} z bazy ${database?.name ?? item.database_id}?`)) return
    await run(async () => {
      await request<{ status: string }>(`/database-users/${item.id}`, { method: 'DELETE' })
      if (editingUser?.id === item.id) setEditingUser(null)
      setMessage(`Użytkownik ${item.username} został usunięty.`)
      await load()
    })
  }

  async function rotatePassword(item: DatabaseUser) {
    if (!window.confirm(`Wygenerować nowe hasło dla użytkownika ${item.username}?`)) return
    await run(async () => {
      const result = await request<{ password: string }>(`/database-users/${item.id}/password`, { method: 'POST' })
      setCreatedCredential({
        username: item.username,
        password: result.password,
        database: databaseById.get(item.database_id)?.name ?? item.database_id,
      })
      setMessage(`Hasło użytkownika ${item.username} zostało zmienione.`)
    })
  }

  async function savePrivileges(event: FormEvent) {
    event.preventDefault()
    if (!editingUser) return
    const current = new Set(editingUser.privileges)
    const next = new Set(editingPrivileges)
    const toGrant = editingPrivileges.filter((privilege) => !current.has(privilege))
    const toRevoke = editingUser.privileges.filter((privilege) => !next.has(privilege))

    await run(async () => {
      let updated = editingUser
      if (toGrant.length > 0) {
        updated = await request<DatabaseUser>(`/database-users/${editingUser.id}/grants`, {
          method: 'POST',
          body: JSON.stringify({ action: 'grant', privileges: toGrant }),
        })
      }
      if (toRevoke.length > 0) {
        updated = await request<DatabaseUser>(`/database-users/${editingUser.id}/grants`, {
          method: 'POST',
          body: JSON.stringify({ action: 'revoke', privileges: toRevoke }),
        })
      }
      setEditingUser(updated)
      setEditingPrivileges(updated.privileges)
      setMessage(`Uprawnienia użytkownika ${updated.username} zostały zaktualizowane.`)
      await load()
    })
  }

  function togglePrivilege(value: string, target: 'new' | 'edit') {
    const setter = target === 'new' ? setNewUserPrivileges : setEditingPrivileges
    setter((current) => current.includes(value) ? current.filter((item) => item !== value) : [...current, value])
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
      <div>
        <h1>Bazy danych</h1>
        <p className="muted">MySQL/MariaDB provisioning, użytkownicy, przypisania, scoped grants i backupy.</p>
      </div>
      <div className="status-chip" data-ok={mysql?.running ? 'true' : 'false'}>
        MySQL: {mysql?.running ? mysql.version || 'connected' : 'unavailable'}
      </div>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}

    {createdCredential && <section className="credential-card database-user-credential">
      <div>
        <strong>Dane użytkownika bazy</strong>
        <p className="muted">Hasło jest pokazywane tylko po utworzeniu użytkownika lub jego zmianie.</p>
      </div>
      <code>
        DB_NAME={createdCredential.database}<br />
        DB_USER={createdCredential.username}<br />
        DB_PASSWORD={createdCredential.password}
      </code>
      <button type="button" className="secondary" onClick={() => setCreatedCredential(null)}>Ukryj</button>
    </section>}

    {canMutate && <div className="database-admin-grid">
      <form className="panel compact-form" onSubmit={createDatabase}>
        <h2>Utwórz bazę</h2>
        <label>Nazwa
          <input value={databaseName} onChange={(event) => setDatabaseName(event.target.value)} placeholder="app_db" required />
        </label>
        <button type="submit" disabled={busy}>Utwórz bazę</button>
      </form>

      <form className="panel compact-form" onSubmit={createDatabaseUser}>
        <h2>Utwórz użytkownika bazy</h2>
        <label>Baza danych
          <select value={newUserDatabaseId} onChange={(event) => setNewUserDatabaseId(event.target.value)} required>
            <option value="">Wybierz bazę</option>
            {databases.map((item) => <option value={item.id} key={item.id}>{item.name}</option>)}
          </select>
        </label>
        <label>Nazwa użytkownika
          <input value={newUsername} onChange={(event) => setNewUsername(event.target.value)} placeholder="app_user" />
        </label>
        <div className="database-privileges">
          <span>Uprawnienia</span>
          <div className="database-privilege-grid">
            {AVAILABLE_PRIVILEGES.map((privilege) => <label className="checkbox" key={privilege}>
              <input type="checkbox" checked={newUserPrivileges.includes(privilege)} onChange={() => togglePrivilege(privilege, 'new')} />
              {privilege}
            </label>)}
          </div>
        </div>
        <button type="submit" disabled={busy || !newUserDatabaseId || newUserPrivileges.length === 0}>Utwórz i przypisz</button>
      </form>
    </div>}

    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Bazy danych</h2>
          <p className="muted">Lista baz oraz liczba przypisanych kont.</p>
        </div>
      </div>
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
                <button type="button" className="secondary" onClick={() => run(() => showBackups(item))}>Backupy</button>
                {canMutate && <button type="button" className="secondary" onClick={() => { setNewUserDatabaseId(item.id); document.getElementById('database-users')?.scrollIntoView({ behavior: 'smooth' }) }} disabled={busy}>Dodaj użytkownika</button>}
                {canMutate && <button type="button" className="secondary" onClick={() => queueBackup(item)} disabled={busy}>Backup</button>}
                {canMutate && <button type="button" className="danger" onClick={() => removeDatabase(item)} disabled={busy}>Usuń</button>}
              </td>
            </tr>)}
            {databases.length === 0 && <tr><td colSpan={7} className="muted">Brak zarządzanych baz.</td></tr>}
          </tbody>
        </table>
      </div>
    </section>

    <section className="panel" id="database-users">
      <div className="section-heading">
        <div>
          <h2>Użytkownicy baz danych</h2>
          <p className="muted">Konta MySQL/MariaDB przypisane do konkretnych baz z ograniczonym zestawem uprawnień.</p>
        </div>
      </div>
      <div className="table-scroll">
        <table>
          <thead><tr><th>Użytkownik</th><th>Baza</th><th>Uprawnienia</th><th>Utworzono</th><th>Akcje</th></tr></thead>
          <tbody>
            {users.map((item) => <tr key={item.id}>
              <td><strong>{item.username}</strong></td>
              <td>{databaseById.get(item.database_id)?.name ?? item.database_id}</td>
              <td><div className="database-grants">{item.privileges.map((privilege) => <span className="badge badge-muted" key={privilege}>{privilege}</span>)}</div></td>
              <td>{new Date(item.created_at).toLocaleString()}</td>
              <td className="actions">
                {canMutate && <button type="button" className="secondary" onClick={() => { setEditingUser(item); setEditingPrivileges(item.privileges) }} disabled={busy}>Uprawnienia</button>}
                {canMutate && <button type="button" className="secondary" onClick={() => rotatePassword(item)} disabled={busy}>Nowe hasło</button>}
                {canMutate && <button type="button" className="danger" onClick={() => removeUser(item)} disabled={busy}>Usuń</button>}
              </td>
            </tr>)}
            {users.length === 0 && <tr><td colSpan={5} className="muted">Brak użytkowników baz danych.</td></tr>}
          </tbody>
        </table>
      </div>
    </section>

    {editingUser && <form className="panel database-grants-editor" onSubmit={savePrivileges}>
      <div className="section-heading">
        <div>
          <h2>Uprawnienia: {editingUser.username}</h2>
          <p className="muted">Baza: {databaseById.get(editingUser.database_id)?.name ?? editingUser.database_id}</p>
        </div>
        <button type="button" className="secondary" onClick={() => setEditingUser(null)}>Zamknij</button>
      </div>
      <div className="database-privilege-grid">
        {AVAILABLE_PRIVILEGES.map((privilege) => <label className="checkbox" key={privilege}>
          <input type="checkbox" checked={editingPrivileges.includes(privilege)} onChange={() => togglePrivilege(privilege, 'edit')} />
          {privilege}
        </label>)}
      </div>
      <div className="form-actions">
        <button type="submit" disabled={busy || editingPrivileges.length === 0}>Zapisz uprawnienia</button>
      </div>
    </form>}

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
  </>
}

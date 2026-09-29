import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, Navigate, useParams, useSearchParams } from 'react-router-dom'
import { request } from '../api/client'
import type {
  DatabaseBackup,
  DatabaseConnectionCredential,
  DatabaseRecord,
  DatabaseUser,
  DatabaseUserCreateResult,
  DatabaseUserPasswordResult,
  Job,
  MySQLPluginStatus,
  PostgreSQLPluginStatus,
} from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Modal } from '../components/Modal'

const MYSQL_PRIVILEGES = [
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'INDEX', 'ALTER',
  'REFERENCES', 'CREATE TEMPORARY TABLES', 'LOCK TABLES', 'EXECUTE',
  'CREATE VIEW', 'SHOW VIEW', 'TRIGGER',
]

const MYSQL_DEFAULT_PRIVILEGES = [
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'INDEX', 'ALTER',
  'REFERENCES', 'CREATE TEMPORARY TABLES', 'LOCK TABLES',
  'CREATE VIEW', 'SHOW VIEW', 'TRIGGER',
]

const POSTGRESQL_PRIVILEGES = [
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'REFERENCES', 'TRIGGER', 'EXECUTE',
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

function belongsToEngine(item: DatabaseRecord, engine: 'mysql' | 'postgresql') {
  const value = item.engine.toLowerCase()
  return engine === 'mysql' ? value === 'mysql' || value === 'mariadb' : value === 'postgresql' || value === 'postgres'
}

export function DatabaseEnginePage() {
  const { user } = useAuth()
  const canMutate = user?.role !== 'viewer'
  const params = useParams<{ engine: string }>()
  const engine = params.engine === 'postgresql' ? 'postgresql' : params.engine === 'mysql' ? 'mysql' : null
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = searchParams.get('tab') === 'users' ? 'users' : 'databases'

  const [databases, setDatabases] = useState<DatabaseRecord[]>([])
  const [users, setUsers] = useState<DatabaseUser[]>([])
  const [serverInstalled, setServerInstalled] = useState(false)
  const [serverRunning, setServerRunning] = useState(false)
  const [serverMessage, setServerMessage] = useState('')
  const [databaseName, setDatabaseName] = useState('')
  const [databaseId, setDatabaseId] = useState(searchParams.get('database') ?? '')
  const [selectedDatabase, setSelectedDatabase] = useState<DatabaseRecord | null>(null)
  const [backups, setBackups] = useState<DatabaseBackup[]>([])
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [generatePassword, setGeneratePassword] = useState(true)
  const [privileges, setPrivileges] = useState<string[]>([])
  const [editingUser, setEditingUser] = useState<DatabaseUser | null>(null)
  const [editingPrivileges, setEditingPrivileges] = useState<string[]>([])
  const [newPassword, setNewPassword] = useState('')
  const [editorMessage, setEditorMessage] = useState('')
  const [editorError, setEditorError] = useState('')
  const [credential, setCredential] = useState<DatabaseConnectionCredential | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const engineLabel = engine === 'postgresql' ? 'PostgreSQL' : 'MySQL / MariaDB'
  const availablePrivileges = engine === 'postgresql' ? POSTGRESQL_PRIVILEGES : MYSQL_PRIVILEGES
  const defaultPrivileges = engine === 'postgresql' ? POSTGRESQL_PRIVILEGES : MYSQL_DEFAULT_PRIVILEGES

  useEffect(() => {
    setPrivileges([...defaultPrivileges])
  }, [engine])

  const load = useCallback(async () => {
    if (!engine) return
    const statusPath = engine === 'postgresql' ? '/plugins/postgresql/status' : '/plugins/mysql/status'
    const [dbs, dbUsers, status] = await Promise.all([
      request<DatabaseRecord[]>('/databases'),
      request<DatabaseUser[]>('/database-users'),
      engine === 'postgresql'
        ? request<PostgreSQLPluginStatus>(statusPath)
        : request<MySQLPluginStatus>(statusPath),
    ])
    const engineDatabases = (dbs ?? []).filter((item) => belongsToEngine(item, engine))
    setDatabases(engineDatabases)
    setUsers(dbUsers ?? [])
    setServerInstalled(Boolean(status.installed))
    setServerRunning(Boolean(status.running))
    setServerMessage(status.message ?? '')
    setDatabaseId((current) => {
      if (current && engineDatabases.some((item) => item.id === current)) return current
      return engineDatabases[0]?.id ?? ''
    })
  }, [engine])

  useEffect(() => {
    load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać danych serwera baz danych'))
  }, [load])

  const databaseById = useMemo(() => new Map(databases.map((item) => [item.id, item])), [databases])
  const engineDatabaseIds = useMemo(() => new Set(databases.map((item) => item.id)), [databases])
  const engineUsers = useMemo(() => users.filter((item) => engineDatabaseIds.has(item.database_id)), [users, engineDatabaseIds])
  const filteredUsers = useMemo(() => databaseId ? engineUsers.filter((item) => item.database_id === databaseId) : engineUsers, [engineUsers, databaseId])
  const userCountByDatabase = useMemo(() => {
    const counts = new Map<string, number>()
    engineUsers.forEach((item) => counts.set(item.database_id, (counts.get(item.database_id) ?? 0) + 1))
    return counts
  }, [engineUsers])

  if (!engine) return <Navigate to="/databases" replace />

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

  function changeDatabase(value: string) {
    setDatabaseId(value)
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'users')
    if (value) next.set('database', value)
    else next.delete('database')
    setSearchParams(next)
  }

  function openUsers(item: DatabaseRecord) {
    setDatabaseId(item.id)
    setSearchParams({ tab: 'users', database: item.id })
  }

  async function createDatabase(event: FormEvent) {
    event.preventDefault()
    if (!databaseName.trim()) return
    await run(async () => {
      const item = await request<DatabaseRecord>('/databases', {
        method: 'POST',
        body: JSON.stringify({
          name: databaseName.trim(),
          engine,
          charset: engine === 'postgresql' ? 'UTF8' : 'utf8mb4',
        }),
      })
      setDatabaseName('')
      setMessage(`Baza ${item.name} została utworzona na serwerze ${engineLabel}.`)
      await load()
    })
  }

  async function removeDatabase(item: DatabaseRecord) {
    if (!window.confirm(`Usunąć bazę ${item.name}? Zarządzani użytkownicy tej bazy zostaną również usunięci.`)) return
    await run(async () => {
      await request<{ status: string }>(`/databases/${item.id}`, { method: 'DELETE' })
      if (selectedDatabase?.id === item.id) {
        setSelectedDatabase(null)
        setBackups([])
      }
      if (databaseId === item.id) setDatabaseId('')
      setMessage(`Baza ${item.name} została usunięta.`)
      await load()
    })
  }

  async function queueBackup(item: DatabaseRecord) {
    await run(async () => {
      const result = await request<{ job: Job; backup: DatabaseBackup }>(`/databases/${item.id}/backup`, { method: 'POST' })
      setMessage(`Backup bazy ${item.name} dodano do kolejki jako zadanie ${result.job.id}.`)
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
      setMessage(`Restore dodano do kolejki jako zadanie ${job.id}.`)
    })
  }

  async function deleteBackup(backup: DatabaseBackup) {
    await run(async () => {
      await request<{ status: string }>(`/database-backups/${backup.id}`, { method: 'DELETE' })
      if (selectedDatabase) await showBackups(selectedDatabase)
    })
  }

  function togglePrivilege(value: string, mode: 'create' | 'edit') {
    const setter = mode === 'create' ? setPrivileges : setEditingPrivileges
    setter((current) => current.includes(value) ? current.filter((item) => item !== value) : [...current, value])
  }

  function openEditor(item: DatabaseUser) {
    setEditingUser(item)
    setEditingPrivileges(item.privileges)
    setNewPassword('')
    setEditorMessage('')
    setEditorError('')
  }

  function closeEditor() {
    if (busy) return
    setEditingUser(null)
    setNewPassword('')
    setEditorMessage('')
    setEditorError('')
  }

  async function createUser(event: FormEvent) {
    event.preventDefault()
    if (!databaseId) return
    await run(async () => {
      const result = await request<DatabaseUserCreateResult>('/database-users', {
        method: 'POST',
        body: JSON.stringify({
          database_id: databaseId,
          username: username.trim(),
          ...(generatePassword ? {} : { password }),
          privileges,
        }),
      })
      setCredential(result.credential)
      setUsername('')
      setPassword('')
      setGeneratePassword(true)
      setPrivileges([...defaultPrivileges])
      setMessage(`Użytkownik ${result.user.username} został utworzony dla bazy ${databaseById.get(databaseId)?.name ?? databaseId}.`)
      await load()
    })
  }

  async function removeUser(item: DatabaseUser) {
    if (!window.confirm(`Usunąć użytkownika ${item.username}?`)) return
    await run(async () => {
      await request<{ status: string }>(`/database-users/${item.id}`, { method: 'DELETE' })
      if (editingUser?.id === item.id) setEditingUser(null)
      setMessage(`Użytkownik ${item.username} został usunięty.`)
      await load()
    })
  }

  async function savePassword(item: DatabaseUser, generate = false) {
    setBusy(true)
    setEditorMessage('')
    setEditorError('')
    try {
      const result = await request<DatabaseUserPasswordResult>(`/database-users/${item.id}/password`, {
        method: 'POST',
        ...(generate ? {} : { body: JSON.stringify({ password: newPassword }) }),
      })
      setCredential(result.credential)
      setNewPassword('')
      setEditorMessage(generate
        ? `Wygenerowano nowe hasło użytkownika ${item.username}.`
        : `Hasło użytkownika ${item.username} zostało zmienione.`)
    } catch (cause) {
      setEditorError(cause instanceof Error ? cause.message : 'Nie udało się zmienić hasła.')
    } finally {
      setBusy(false)
    }
  }

  async function savePrivileges(event: FormEvent) {
    event.preventDefault()
    if (!editingUser) return
    const current = new Set(editingUser.privileges)
    const next = new Set(editingPrivileges)
    const toGrant = editingPrivileges.filter((item) => !current.has(item))
    const toRevoke = editingUser.privileges.filter((item) => !next.has(item))
    setBusy(true)
    setEditorMessage('')
    setEditorError('')
    try {
      let updated = editingUser
      if (toGrant.length) {
        updated = await request<DatabaseUser>(`/database-users/${editingUser.id}/grants`, {
          method: 'POST',
          body: JSON.stringify({ action: 'grant', privileges: toGrant }),
        })
      }
      if (toRevoke.length) {
        updated = await request<DatabaseUser>(`/database-users/${editingUser.id}/grants`, {
          method: 'POST',
          body: JSON.stringify({ action: 'revoke', privileges: toRevoke }),
        })
      }
      setEditingUser(updated)
      setEditingPrivileges(updated.privileges)
      setEditorMessage('Uprawnienia zostały zaktualizowane.')
      await load()
    } catch (cause) {
      setEditorError(cause instanceof Error ? cause.message : 'Nie udało się zaktualizować uprawnień.')
    } finally {
      setBusy(false)
    }
  }

  const managerPath = `/databases/${engine}`
  const usersPath = databaseId
    ? `${managerPath}?tab=users&database=${encodeURIComponent(databaseId)}`
    : `${managerPath}?tab=users`

  return <>
    <div className="page-heading">
      <div>
        <h1>{engineLabel}</h1>
        <p className="muted">Zarządzanie bazami, użytkownikami, hasłami, grantami i backupami serwera {engineLabel}.</p>
      </div>
      <div className="actions">
        <span className="status-chip" data-ok={serverRunning ? 'true' : 'false'}>
          {!serverInstalled ? 'Niezainstalowany' : serverRunning ? 'Działa' : 'Zatrzymany'}
        </span>
        <Link className="button-link secondary" to="/databases">Wróć do serwerów SQL</Link>
      </div>
    </div>

    {serverMessage && <div className="operation-state">{serverMessage}</div>}
    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}
    {credential && <section className="credential-card">
      <div>
        <strong>Dane dostępowe</strong>
        <p className="muted">Hasło jest pokazywane po utworzeniu lub zmianie użytkownika.</p>
      </div>
      <code>DB_DRIVER={credential.engine}<br />DB_HOST={credential.host}<br />DB_PORT={credential.port}<br />DB_DATABASE={credential.database}<br />DB_USERNAME={credential.username}<br />DB_PASSWORD={credential.password}</code>
      <button type="button" className="secondary" onClick={() => setCredential(null)}>Ukryj</button>
    </section>}

    <nav className="tabs" aria-label="Zarządzanie bazami danych">
      <Link className={tab === 'databases' ? 'active' : ''} to={managerPath}>Bazy danych</Link>
      <Link className={tab === 'users' ? 'active' : ''} to={usersPath}>Użytkownicy</Link>
    </nav>

    {tab === 'databases' && <>
      {!serverRunning && <div className="error-banner">
        Serwer {engineLabel} nie działa. Operacje tworzenia i usuwania baz wymagają uruchomionego serwera. <Link to="/plugins">Przejdź do Pluginów</Link>.
      </div>}

      {canMutate && <form className="panel compact-form" onSubmit={createDatabase}>
        <h2>Utwórz bazę</h2>
        <label>Nazwa<input value={databaseName} onChange={(event) => setDatabaseName(event.target.value)} placeholder={engine === 'postgresql' ? 'app_postgres' : 'app_mysql'} required /></label>
        <button type="submit" disabled={busy || !serverRunning}>Utwórz bazę</button>
      </form>}

      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Lista baz danych</h2>
            <p className="muted">{databases.length} zarządzanych baz na serwerze {engineLabel}.</p>
          </div>
          <button type="button" className="secondary" onClick={() => load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się odświeżyć danych'))}>Odśwież</button>
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
                  <button type="button" className="secondary" onClick={() => openUsers(item)}>Użytkownicy</button>
                  <button type="button" className="secondary" onClick={() => run(() => showBackups(item))}>Backupy</button>
                  {canMutate && <button type="button" className="secondary" onClick={() => queueBackup(item)} disabled={busy || !serverRunning}>Backup</button>}
                  {canMutate && <button type="button" className="danger" onClick={() => removeDatabase(item)} disabled={busy || !serverRunning}>Usuń</button>}
                </td>
              </tr>)}
              {databases.length === 0 && <tr><td colSpan={7} className="muted">Brak zarządzanych baz na tym serwerze.</td></tr>}
            </tbody>
          </table>
        </div>
      </section>

      {selectedDatabase && <section className="panel backup-panel">
        <div className="section-heading">
          <div>
            <h2>Backupy: {selectedDatabase.name}</h2>
            <p className="muted">Backup i restore są wykonywane przez Job Engine właściwego silnika SQL.</p>
          </div>
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
                  {canMutate && backup.status === 'ready' && <button type="button" className="secondary" onClick={() => restoreBackup(backup)} disabled={busy || !serverRunning}>Restore</button>}
                  {canMutate && <button type="button" className="danger" onClick={() => deleteBackup(backup)} disabled={busy}>Usuń</button>}
                </td>
              </tr>)}
              {backups.length === 0 && <tr><td colSpan={5} className="muted">Brak backupów.</td></tr>}
            </tbody>
          </table>
        </div>
      </section>}
    </>}

    {tab === 'users' && <>
      {!serverRunning && <div className="error-banner">
        Serwer {engineLabel} nie działa. Zarządzanie użytkownikami wymaga uruchomionego serwera. <Link to="/plugins">Przejdź do Pluginów</Link>.
      </div>}

      {canMutate && <form className="panel form-grid" onSubmit={createUser}>
        <div className="span-2">
          <h2>Utwórz użytkownika</h2>
          <p className="muted">Konto zostanie utworzone rzeczywiście na serwerze {engineLabel} i przypisane do wybranej bazy.</p>
        </div>
        <label>Baza danych<select value={databaseId} onChange={(event) => changeDatabase(event.target.value)} required>
          <option value="">Wybierz bazę</option>
          {databases.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
        </select></label>
        <label>Nazwa użytkownika<input value={username} onChange={(event) => setUsername(event.target.value)} placeholder="app_user" /></label>
        <label className="span-2">Hasło<input type="password" value={password} disabled={generatePassword} onChange={(event) => setPassword(event.target.value)} placeholder={generatePassword ? 'Hasło zostanie wygenerowane automatycznie' : 'Wpisz hasło lub pozostaw puste'} /></label>
        <label className="checkbox span-2"><input type="checkbox" checked={generatePassword} onChange={(event) => setGeneratePassword(event.target.checked)} /> Wygeneruj bezpieczne hasło automatycznie</label>
        <div className="span-2 database-privileges">
          <span>Uprawnienia</span>
          <div className="database-privilege-grid">
            {availablePrivileges.map((item) => <label className="checkbox" key={item}><input type="checkbox" checked={privileges.includes(item)} onChange={() => togglePrivilege(item, 'create')} />{item}</label>)}
          </div>
        </div>
        <div className="form-actions"><button type="submit" disabled={busy || !serverRunning || !databaseId || privileges.length === 0}>Utwórz użytkownika</button></div>
      </form>}

      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Lista użytkowników</h2>
            <p className="muted">Konta zarządzane przez DevBox dla serwera {engineLabel}.</p>
          </div>
          <label>Filtr bazy<select value={databaseId} onChange={(event) => changeDatabase(event.target.value)}>
            <option value="">Wszystkie bazy</option>
            {databases.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select></label>
        </div>
        <div className="table-scroll">
          <table>
            <thead><tr><th>Użytkownik</th><th>Baza</th><th>Uprawnienia</th><th>Utworzono</th><th>Akcje</th></tr></thead>
            <tbody>
              {filteredUsers.map((item) => <tr key={item.id}>
                <td><strong>{item.username}</strong></td>
                <td>{databaseById.get(item.database_id)?.name ?? item.database_id}</td>
                <td><div className="database-grants">{item.privileges.map((privilege) => <span className="badge badge-muted" key={privilege}>{privilege}</span>)}</div></td>
                <td>{new Date(item.created_at).toLocaleString()}</td>
                <td className="actions">
                  {canMutate && <button type="button" className="secondary" onClick={() => openEditor(item)} disabled={!serverRunning}>Edytuj</button>}
                  {canMutate && <button type="button" className="danger" onClick={() => removeUser(item)} disabled={busy || !serverRunning}>Usuń</button>}
                </td>
              </tr>)}
              {filteredUsers.length === 0 && <tr><td colSpan={5} className="muted">Brak użytkowników dla wybranego filtra.</td></tr>}
            </tbody>
          </table>
        </div>
      </section>
    </>}

    <Modal open={editingUser !== null} onClose={closeEditor} labelId="database-engine-user-edit-title" className="database-user-edit-modal">
      {editingUser && <div className="stack">
        <div className="modal-heading">
          <div>
            <h2 id="database-engine-user-edit-title">Edytuj użytkownika: {editingUser.username}</h2>
            <p className="muted">{databaseById.get(editingUser.database_id)?.name ?? editingUser.database_id} · {engineLabel}</p>
          </div>
          <button type="button" className="secondary" disabled={busy} onClick={closeEditor}>Zamknij</button>
        </div>

        {editorMessage && <div className="success-banner" role="status">{editorMessage}</div>}
        {editorError && <div className="error-banner" role="alert">{editorError}</div>}

        <form className="stack" onSubmit={savePrivileges}>
          <div>
            <h3>Uprawnienia</h3>
            <div className="database-privilege-grid">
              {availablePrivileges.map((item) => <label className="checkbox" key={item}><input type="checkbox" checked={editingPrivileges.includes(item)} onChange={() => togglePrivilege(item, 'edit')} />{item}</label>)}
            </div>
          </div>
          <div className="form-actions"><button type="submit" disabled={busy || editingPrivileges.length === 0}>Zapisz uprawnienia</button></div>
        </form>

        <div className="form-grid database-password-editor">
          <div className="span-2">
            <h3>Zmiana hasła</h3>
            <p className="muted small">Zmiana jest wykonywana bezpośrednio na serwerze {engineLabel}, a sekret DevBox jest aktualizowany po sukcesie.</p>
          </div>
          <label className="span-2">Nowe hasło<input type="password" value={newPassword} onChange={(event) => { setNewPassword(event.target.value); setEditorMessage(''); setEditorError('') }} placeholder="Nowe hasło" /></label>
          <div className="form-actions span-2">
            <button type="button" className="secondary" disabled={busy} onClick={() => void savePassword(editingUser)}>Ustaw hasło</button>
            <button type="button" className="secondary" disabled={busy} onClick={() => void savePassword(editingUser, true)}>Wygeneruj nowe hasło</button>
          </div>
        </div>
      </div>}
    </Modal>
  </>
}

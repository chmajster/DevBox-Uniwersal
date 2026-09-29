import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, Navigate, useParams } from 'react-router-dom'
import { request } from '../api/client'
import type { DatabaseConnectionCredential, DatabaseRecord, DatabaseUser, DatabaseUserPasswordResult } from '../api/types'
import { useAuth } from '../auth/AuthContext'

const MYSQL_PRIVILEGES = [
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'INDEX', 'ALTER',
  'REFERENCES', 'CREATE TEMPORARY TABLES', 'LOCK TABLES', 'EXECUTE',
  'CREATE VIEW', 'SHOW VIEW', 'TRIGGER',
]

const POSTGRESQL_PRIVILEGES = [
  'SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'REFERENCES', 'TRIGGER', 'EXECUTE',
]

function belongsToEngine(item: DatabaseRecord, engine: 'mysql' | 'postgresql') {
  const value = item.engine.toLowerCase()
  return engine === 'mysql' ? value === 'mysql' || value === 'mariadb' : value === 'postgresql' || value === 'postgres'
}

export function DatabaseUserAccountPage() {
  const { user: sessionUser } = useAuth()
  const canMutate = sessionUser?.role !== 'viewer'
  const params = useParams<{ engine: string; userId: string }>()
  const engine = params.engine === 'postgresql' ? 'postgresql' : params.engine === 'mysql' ? 'mysql' : null
  const userId = params.userId ?? ''

  const [account, setAccount] = useState<DatabaseUser | null>(null)
  const [databases, setDatabases] = useState<DatabaseRecord[]>([])
  const [selectedDatabaseId, setSelectedDatabaseId] = useState('')
  const [selectedPrivileges, setSelectedPrivileges] = useState<string[]>([])
  const [newPassword, setNewPassword] = useState('')
  const [credential, setCredential] = useState<DatabaseConnectionCredential | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const availablePrivileges = engine === 'postgresql' ? POSTGRESQL_PRIVILEGES : MYSQL_PRIVILEGES
  const engineLabel = engine === 'postgresql' ? 'PostgreSQL' : 'MySQL / MariaDB'

  const load = useCallback(async () => {
    if (!engine || !userId) return
    const [current, dbs] = await Promise.all([
      request<DatabaseUser>(`/database-users/${encodeURIComponent(userId)}`),
      request<DatabaseRecord[]>('/databases'),
    ])
    setAccount(current)
    const sameEngine = (dbs ?? []).filter((item) => belongsToEngine(item, engine))
    setDatabases(sameEngine)
    setSelectedDatabaseId((currentId) => {
      if (currentId && sameEngine.some((item) => item.id === currentId)) return currentId
      return current.databases[0]?.database_id ?? sameEngine[0]?.id ?? ''
    })
  }, [engine, userId])

  useEffect(() => {
    void load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać konta użytkownika'))
  }, [load])

  const selectedGrant = useMemo(
    () => account?.databases.find((item) => item.database_id === selectedDatabaseId),
    [account, selectedDatabaseId],
  )

  useEffect(() => {
    setSelectedPrivileges(selectedGrant ? [...selectedGrant.privileges] : [])
  }, [selectedGrant, selectedDatabaseId])

  if (!engine) return <Navigate to="/databases" replace />
  if (account && account.engine !== engine && !(engine === 'mysql' && account.engine === 'mariadb')) {
    return <Navigate to={`/databases/${engine}?tab=users`} replace />
  }

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

  function togglePrivilege(privilege: string) {
    setSelectedPrivileges((current) =>
      current.includes(privilege)
        ? current.filter((item) => item !== privilege)
        : [...current, privilege],
    )
  }

  function changeDatabase(databaseId: string) {
    setSelectedDatabaseId(databaseId)
    const grant = account?.databases.find((item) => item.database_id === databaseId)
    setSelectedPrivileges(grant ? [...grant.privileges] : [])
    setMessage('')
    setError('')
  }

  async function saveDatabaseAccess(event: FormEvent) {
    event.preventDefault()
    if (!account || !selectedDatabaseId || selectedPrivileges.length === 0) return
    await run(async () => {
      const updated = await request<DatabaseUser>(
        `/database-users/${encodeURIComponent(account.id)}/databases/${encodeURIComponent(selectedDatabaseId)}`,
        { method: 'PUT', body: JSON.stringify({ privileges: selectedPrivileges }) },
      )
      setAccount(updated)
      const database = databases.find((item) => item.id === selectedDatabaseId)
      setMessage(`Dostęp do bazy ${database?.name ?? selectedDatabaseId} został zapisany.`)
    })
  }

  async function removeDatabaseAccess() {
    if (!account || !selectedDatabaseId || !selectedGrant) return
    const database = databases.find((item) => item.id === selectedDatabaseId)
    if (!window.confirm(`Usunąć użytkownikowi ${account.username} dostęp do bazy ${database?.name ?? selectedDatabaseId}?`)) return
    await run(async () => {
      const updated = await request<DatabaseUser>(
        `/database-users/${encodeURIComponent(account.id)}/databases/${encodeURIComponent(selectedDatabaseId)}`,
        { method: 'DELETE' },
      )
      setAccount(updated)
      setSelectedPrivileges([])
      setMessage(`Dostęp do bazy ${database?.name ?? selectedDatabaseId} został usunięty.`)
    })
  }

  async function changePassword(generate: boolean) {
    if (!account) return
    await run(async () => {
      const result = await request<DatabaseUserPasswordResult>(`/database-users/${encodeURIComponent(account.id)}/password`, {
        method: 'POST',
        ...(generate ? {} : { body: JSON.stringify({ password: newPassword }) }),
      })
      setCredential(result.credential)
      setNewPassword('')
      setMessage(generate ? 'Wygenerowano i ustawiono nowe hasło.' : 'Hasło użytkownika zostało zmienione.')
    })
  }

  return <>
    <div className="page-heading">
      <div>
        <h1>{account?.username ?? 'Konto użytkownika'}</h1>
        <p className="muted">Konto na serwerze {engineLabel}: hasło, przypisane bazy i uprawnienia per baza.</p>
      </div>
      <Link className="button-link secondary" to={`/databases/${engine}?tab=users`}>Wróć do użytkowników</Link>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}
    {credential && <section className="credential-card">
      <div><strong>Nowe dane dostępowe</strong><p className="muted">Hasło jest pokazywane tylko po zmianie lub wygenerowaniu.</p></div>
      <code>DB_HOST={credential.host}<br />DB_PORT={credential.port}<br />DB_DATABASE={credential.database || 'wybierz_przypisana_baze'}<br />DB_USERNAME={credential.username}<br />DB_PASSWORD={credential.password}</code>
      <button type="button" className="secondary" onClick={() => setCredential(null)}>Ukryj</button>
    </section>}

    {!account && !error && <section className="panel"><p className="muted">Ładowanie konta…</p></section>}

    {account && <>
      <section className="panel">
        <div className="section-heading">
          <div>
            <h2>Przypisane bazy</h2>
            <p className="muted">Każda baza ma własny zestaw uprawnień dla tego samego konta SQL.</p>
          </div>
          <span className="badge badge-muted">{account.databases.length} baz</span>
        </div>
        <div className="table-scroll">
          <table>
            <thead><tr><th>Baza</th><th>Silnik</th><th>Uprawnienia</th><th>Akcja</th></tr></thead>
            <tbody>
              {account.databases.map((grant) => <tr key={grant.database_id}>
                <td><strong>{grant.database_name}</strong></td>
                <td>{grant.engine}</td>
                <td><div className="database-grants">{grant.privileges.map((privilege) => <span className="badge badge-muted" key={privilege}>{privilege}</span>)}</div></td>
                <td><button type="button" className="secondary" onClick={() => changeDatabase(grant.database_id)}>Konfiguruj</button></td>
              </tr>)}
              {account.databases.length === 0 && <tr><td colSpan={4} className="muted">Użytkownik nie ma jeszcze dostępu do żadnej bazy.</td></tr>}
            </tbody>
          </table>
        </div>
      </section>

      <form className="panel stack" onSubmit={saveDatabaseAccess}>
        <div className="section-heading">
          <div>
            <h2>Dostęp do bazy</h2>
            <p className="muted">Wybierz dowolną bazę z serwera i ustaw dokładny zestaw uprawnień użytkownika.</p>
          </div>
        </div>
        <label>Baza danych
          <select value={selectedDatabaseId} onChange={(event) => changeDatabase(event.target.value)} disabled={busy || !canMutate}>
            <option value="">Wybierz bazę</option>
            {databases.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
          </select>
        </label>
        <div className="database-privileges">
          <span>Uprawnienia</span>
          <div className="database-privilege-grid">
            {availablePrivileges.map((privilege) => <label className="checkbox" key={privilege}>
              <input
                type="checkbox"
                checked={selectedPrivileges.includes(privilege)}
                disabled={busy || !canMutate}
                onChange={() => togglePrivilege(privilege)}
              />
              {privilege}
            </label>)}
          </div>
        </div>
        {canMutate && <div className="form-actions">
          <button type="submit" disabled={busy || !selectedDatabaseId || selectedPrivileges.length === 0}>
            {selectedGrant ? 'Zapisz uprawnienia' : 'Przypisz bazę'}
          </button>
          {selectedGrant && <button type="button" className="danger" disabled={busy} onClick={() => void removeDatabaseAccess()}>Usuń dostęp do bazy</button>}
        </div>}
      </form>

      <section className="panel form-grid database-password-editor">
        <div className="span-2">
          <h2>Hasło użytkownika</h2>
          <p className="muted">Hasło dotyczy konta {account.username} na całym serwerze {engineLabel}, niezależnie od liczby przypisanych baz.</p>
        </div>
        <label className="span-2">Nowe hasło
          <input type="password" value={newPassword} disabled={busy || !canMutate} onChange={(event) => setNewPassword(event.target.value)} placeholder="Minimum 8 znaków albo puste hasło" />
        </label>
        {canMutate && <div className="form-actions span-2">
          <button type="button" className="secondary" disabled={busy} onClick={() => void changePassword(false)}>Ustaw hasło</button>
          <button type="button" className="secondary" disabled={busy} onClick={() => void changePassword(true)}>Wygeneruj nowe hasło</button>
        </div>}
      </section>
    </>}
  </>
}

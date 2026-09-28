import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { request } from '../api/client'
import type { DatabaseRecord, DatabaseUser, DatabaseUserCreateResult } from '../api/types'
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

export function DatabaseUsersPage() {
  const { user } = useAuth()
  const canMutate = user?.role !== 'viewer'
  const [params, setParams] = useSearchParams()
  const [databases, setDatabases] = useState<DatabaseRecord[]>([])
  const [users, setUsers] = useState<DatabaseUser[]>([])
  const [databaseId, setDatabaseId] = useState(params.get('database') ?? '')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [privileges, setPrivileges] = useState<string[]>(DEFAULT_PRIVILEGES)
  const [editingUser, setEditingUser] = useState<DatabaseUser | null>(null)
  const [editingPrivileges, setEditingPrivileges] = useState<string[]>([])
  const [newPassword, setNewPassword] = useState('')
  const [credential, setCredential] = useState<{ database: string; username: string; password: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const databaseById = useMemo(() => new Map(databases.map((item) => [item.id, item])), [databases])
  const filteredUsers = useMemo(() => databaseId ? users.filter((item) => item.database_id === databaseId) : users, [users, databaseId])

  const load = useCallback(async () => {
    const [dbs, dbUsers] = await Promise.all([
      request<DatabaseRecord[]>('/databases'),
      request<DatabaseUser[]>('/database-users'),
    ])
    setDatabases(dbs ?? [])
    setUsers(dbUsers ?? [])
    setDatabaseId((current) => current || dbs?.[0]?.id || '')
  }, [])

  useEffect(() => {
    load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać użytkowników baz'))
  }, [load])

  async function run(action: () => Promise<void>) {
    setBusy(true); setError(''); setMessage('')
    try { await action() }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Operacja nie powiodła się') }
    finally { setBusy(false) }
  }

  function changeDatabase(value: string) {
    setDatabaseId(value)
    setParams(value ? { database: value } : {})
  }

  function togglePrivilege(value: string, mode: 'create' | 'edit') {
    const setter = mode === 'create' ? setPrivileges : setEditingPrivileges
    setter((current) => current.includes(value) ? current.filter((item) => item !== value) : [...current, value])
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
          password,
          privileges,
        }),
      })
      setCredential({
        database: databaseById.get(databaseId)?.name ?? databaseId,
        username: result.credential.username,
        password: result.credential.password,
      })
      setUsername('')
      setPassword('')
      setPrivileges(DEFAULT_PRIVILEGES)
      setMessage(`Użytkownik ${result.user.username} został utworzony i przypisany do bazy.`)
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

  async function savePassword(item: DatabaseUser) {
    await run(async () => {
      const result = await request<{ password: string }>(`/database-users/${item.id}/password`, {
        method: 'POST',
        body: JSON.stringify({ password: newPassword }),
      })
      setCredential({
        database: databaseById.get(item.database_id)?.name ?? item.database_id,
        username: item.username,
        password: result.password,
      })
      setNewPassword('')
      setMessage(`Hasło użytkownika ${item.username} zostało zmienione.`)
    })
  }

  async function savePrivileges(event: FormEvent) {
    event.preventDefault()
    if (!editingUser) return
    const current = new Set(editingUser.privileges)
    const next = new Set(editingPrivileges)
    const toGrant = editingPrivileges.filter((p) => !current.has(p))
    const toRevoke = editingUser.privileges.filter((p) => !next.has(p))
    await run(async () => {
      let updated = editingUser
      if (toGrant.length) {
        updated = await request<DatabaseUser>(`/database-users/${editingUser.id}/grants`, {
          method: 'POST', body: JSON.stringify({ action: 'grant', privileges: toGrant }),
        })
      }
      if (toRevoke.length) {
        updated = await request<DatabaseUser>(`/database-users/${editingUser.id}/grants`, {
          method: 'POST', body: JSON.stringify({ action: 'revoke', privileges: toRevoke }),
        })
      }
      setEditingUser(updated)
      setEditingPrivileges(updated.privileges)
      setMessage('Uprawnienia zostały zaktualizowane.')
      await load()
    })
  }

  return <>
    <div className="page-heading">
      <div><h1>Użytkownicy baz danych</h1><p className="muted">Tworzenie kont MySQL/MariaDB, hasła, przypisania do baz i scoped grants.</p></div>
      <Link className="button-link secondary" to="/databases">Wróć do baz danych</Link>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}
    {credential && <section className="credential-card">
      <div><strong>Dane dostępowe</strong><p className="muted">Hasło jest pokazywane po utworzeniu lub zmianie.</p></div>
      <code>DB_NAME={credential.database}<br />DB_USER={credential.username}<br />DB_PASSWORD={credential.password}</code>
      <button type="button" className="secondary" onClick={() => setCredential(null)}>Ukryj</button>
    </section>}

    {canMutate && <form className="panel form-grid" onSubmit={createUser}>
      <div className="span-2"><h2>Utwórz użytkownika</h2></div>
      <label>Baza danych<select value={databaseId} onChange={(e) => changeDatabase(e.target.value)} required>
        <option value="">Wybierz bazę</option>{databases.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </select></label>
      <label>Nazwa użytkownika<input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="app_user" /></label>
      <label className="span-2">Hasło<input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Min. 8 znaków; puste = wygeneruj automatycznie" /></label>
      <div className="span-2 database-privileges">
        <span>Uprawnienia</span>
        <div className="database-privilege-grid">{AVAILABLE_PRIVILEGES.map((p) => <label className="checkbox" key={p}><input type="checkbox" checked={privileges.includes(p)} onChange={() => togglePrivilege(p, 'create')} />{p}</label>)}</div>
      </div>
      <div className="form-actions"><button type="submit" disabled={busy || !databaseId || privileges.length === 0}>Utwórz użytkownika</button></div>
    </form>}

    <section className="panel">
      <div className="section-heading">
        <div><h2>Lista użytkowników</h2><p className="muted">Filtruj po bazie lub pokaż wszystkie konta.</p></div>
        <label>Filtr bazy<select value={databaseId} onChange={(e) => changeDatabase(e.target.value)}><option value="">Wszystkie bazy</option>{databases.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      </div>
      <div className="table-scroll">
        <table>
          <thead><tr><th>Użytkownik</th><th>Baza</th><th>Uprawnienia</th><th>Utworzono</th><th>Akcje</th></tr></thead>
          <tbody>
            {filteredUsers.map((item) => <tr key={item.id}>
              <td><strong>{item.username}</strong></td>
              <td>{databaseById.get(item.database_id)?.name ?? item.database_id}</td>
              <td><div className="database-grants">{item.privileges.map((p) => <span className="badge badge-muted" key={p}>{p}</span>)}</div></td>
              <td>{new Date(item.created_at).toLocaleString()}</td>
              <td className="actions">
                {canMutate && <button type="button" className="secondary" onClick={() => { setEditingUser(item); setEditingPrivileges(item.privileges); setNewPassword('') }}>Edytuj</button>}
                {canMutate && <button type="button" className="danger" onClick={() => removeUser(item)} disabled={busy}>Usuń</button>}
              </td>
            </tr>)}
            {filteredUsers.length === 0 && <tr><td colSpan={5} className="muted">Brak użytkowników dla wybranego filtra.</td></tr>}
          </tbody>
        </table>
      </div>
    </section>

    {editingUser && <section className="panel stack">
      <div className="section-heading"><div><h2>Edytuj: {editingUser.username}</h2><p className="muted">{databaseById.get(editingUser.database_id)?.name}</p></div><button type="button" className="secondary" onClick={() => setEditingUser(null)}>Zamknij</button></div>
      <form className="stack" onSubmit={savePrivileges}>
        <div className="database-privilege-grid">{AVAILABLE_PRIVILEGES.map((p) => <label className="checkbox" key={p}><input type="checkbox" checked={editingPrivileges.includes(p)} onChange={() => togglePrivilege(p, 'edit')} />{p}</label>)}</div>
        <div className="form-actions"><button type="submit" disabled={busy || editingPrivileges.length === 0}>Zapisz uprawnienia</button></div>
      </form>
      <div className="form-grid">
        <label className="span-2">Nowe hasło<input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} placeholder="Min. 8 znaków; puste = wygeneruj automatycznie" /></label>
        <div className="form-actions"><button type="button" className="secondary" disabled={busy} onClick={() => savePassword(editingUser)}>{newPassword ? 'Ustaw hasło' : 'Wygeneruj nowe hasło'}</button></div>
      </div>
    </section>}
  </>
}

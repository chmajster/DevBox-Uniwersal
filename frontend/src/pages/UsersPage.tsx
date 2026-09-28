import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { request } from '../api/client'
import type { Role, User } from '../api/types'
import { useAuth } from '../auth/AuthContext'

const roleLabels: Record<Role, string> = {
  admin: 'Administrator',
  operator: 'Operator',
  viewer: 'Viewer',
}

interface UserCreateResult {
  user: User
  password: string
}

interface PasswordChangeResult {
  status: string
  password: string
  sessions_revoked: boolean
}

export function UsersPage() {
  const { user: currentUser } = useAuth()
  const [users, setUsers] = useState<User[]>([])
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<Role>('viewer')
  const [active, setActive] = useState(true)
  const [password, setPassword] = useState('')
  const [generatePassword, setGeneratePassword] = useState(true)
  const [editing, setEditing] = useState<User | null>(null)
  const [editingRole, setEditingRole] = useState<Role>('viewer')
  const [editingActive, setEditingActive] = useState(true)
  const [newPassword, setNewPassword] = useState('')
  const [credential, setCredential] = useState<{ username: string; password: string } | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const activeAdmins = useMemo(() => users.filter((item) => item.role === 'admin' && item.active).length, [users])

  const load = useCallback(async () => {
    const items = await request<User[]>('/users')
    setUsers(items ?? [])
    setEditing((current) => current ? (items ?? []).find((item) => item.id === current.id) ?? null : null)
  }, [])

  useEffect(() => {
    void load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać użytkowników'))
  }, [load])

  async function run(name: string, action: () => Promise<void>) {
    setBusy(name)
    setError('')
    setMessage('')
    try {
      await action()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Operacja użytkownika nie powiodła się')
    } finally {
      setBusy('')
    }
  }

  async function createUser(event: FormEvent) {
    event.preventDefault()
    await run('create', async () => {
      const result = await request<UserCreateResult>('/users', {
        method: 'POST',
        body: JSON.stringify({
          username: username.trim(),
          role,
          active,
          generate_password: generatePassword,
          ...(generatePassword ? {} : { password }),
        }),
      })
      setCredential({ username: result.user.username, password: result.password })
      setUsername('')
      setRole('viewer')
      setActive(true)
      setPassword('')
      setGeneratePassword(true)
      setMessage(`Użytkownik ${result.user.username} został utworzony.`)
      await load()
    })
  }

  function editUser(item: User) {
    setEditing(item)
    setEditingRole(item.role)
    setEditingActive(item.active)
    setNewPassword('')
    setCredential(null)
    setError('')
    setMessage('')
  }

  async function saveAccess(event: FormEvent) {
    event.preventDefault()
    if (!editing) return
    await run('access', async () => {
      const updated = await request<User>(`/users/${encodeURIComponent(editing.id)}`, {
        method: 'PATCH',
        body: JSON.stringify({ role: editingRole, active: editingActive }),
      })
      setEditing(updated)
      setEditingRole(updated.role)
      setEditingActive(updated.active)
      setMessage('Rola i status użytkownika zostały zapisane. Jego aktywne sesje zostały unieważnione.')
      await load()
    })
  }

  async function changePassword(generate: boolean) {
    if (!editing) return
    await run(generate ? 'generate-password' : 'password', async () => {
      const result = await request<PasswordChangeResult>(`/users/${encodeURIComponent(editing.id)}/password`, {
        method: 'POST',
        body: JSON.stringify(generate ? { generate: true } : { password: newPassword, generate: false }),
      })
      setCredential({ username: editing.username, password: result.password })
      setNewPassword('')
      setMessage('Hasło zostało zmienione. Wszystkie sesje tego użytkownika zostały unieważnione.')
    })
  }

  async function revokeSessions(item: User) {
    if (!window.confirm(`Unieważnić wszystkie aktywne sesje użytkownika ${item.username}?`)) return
    await run('sessions', async () => {
      await request<{ status: string }>(`/users/${encodeURIComponent(item.id)}/sessions/revoke`, {
        method: 'POST',
        body: '{}',
      })
      setMessage(`Sesje użytkownika ${item.username} zostały unieważnione.`)
    })
  }

  async function removeUser(item: User) {
    if (!window.confirm(`Usunąć użytkownika ${item.username}? Tej operacji nie można cofnąć.`)) return
    await run('delete', async () => {
      await request<{ status: string }>(`/users/${encodeURIComponent(item.id)}`, { method: 'DELETE' })
      if (editing?.id === item.id) setEditing(null)
      setMessage(`Użytkownik ${item.username} został usunięty.`)
      await load()
    })
  }

  const editingSelf = editing?.id === currentUser?.id
  const editingLastAdmin = Boolean(editing?.role === 'admin' && editing.active && activeAdmins <= 1)

  return <>
    <div className="page-heading">
      <div>
        <h1>Użytkownicy</h1>
        <p className="muted">Konta panelu DevBox, role, aktywność, hasła i aktywne sesje.</p>
      </div>
      <span className="status-chip" data-ok="true">{users.length} kont</span>
    </div>

    {error && <div className="error-banner" role="alert">{error}</div>}
    {message && <div className="success-banner" role="status">{message}</div>}
    {credential && <section className="credential-card">
      <div>
        <strong>Nowe dane logowania</strong>
        <p className="muted">Hasło jest pokazywane tylko po utworzeniu lub zmianie. Zapisz je przed ukryciem.</p>
      </div>
      <code>USER={credential.username}<br />PASSWORD={credential.password}</code>
      <button type="button" className="secondary" onClick={() => setCredential(null)}>Ukryj</button>
    </section>}

    <form className="panel form-grid" onSubmit={createUser}>
      <div className="span-2">
        <h2>Utwórz użytkownika</h2>
        <p className="muted">Nowe konto może otrzymać rolę Administrator, Operator albo Viewer.</p>
      </div>
      <label>Nazwa użytkownika
        <input required maxLength={120} value={username} onChange={(event) => setUsername(event.target.value)} placeholder="np. operator01" />
      </label>
      <label>Rola
        <select value={role} onChange={(event) => setRole(event.target.value as Role)}>
          <option value="viewer">Viewer</option>
          <option value="operator">Operator</option>
          <option value="admin">Administrator</option>
        </select>
      </label>
      <label className="checkbox">
        <input type="checkbox" checked={active} onChange={(event) => setActive(event.target.checked)} />
        Konto aktywne
      </label>
      <label className="span-2">Hasło
        <input type="password" minLength={12} disabled={generatePassword} required={!generatePassword}
          value={password} onChange={(event) => setPassword(event.target.value)}
          placeholder={generatePassword ? 'Hasło zostanie wygenerowane automatycznie' : 'Minimum 12 znaków'} />
      </label>
      <label className="checkbox span-2">
        <input type="checkbox" checked={generatePassword} onChange={(event) => setGeneratePassword(event.target.checked)} />
        Wygeneruj bezpieczne hasło
      </label>
      <div className="form-actions">
        <button type="submit" disabled={busy !== '' || !username.trim() || (!generatePassword && password.length < 12)}>
          {busy === 'create' ? 'Tworzenie…' : 'Utwórz użytkownika'}
        </button>
      </div>
    </form>

    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Lista użytkowników</h2>
          <p className="muted">Zmiana roli, wyłączenie konta lub zmiana hasła unieważnia istniejące sesje zgodnie z operacją.</p>
        </div>
      </div>
      <div className="table-scroll">
        <table>
          <thead><tr><th>Użytkownik</th><th>Rola</th><th>Status</th><th>Utworzono</th><th>Zmieniono</th><th>Akcje</th></tr></thead>
          <tbody>
            {users.map((item) => <tr key={item.id}>
              <td><strong>{item.username}</strong>{item.id === currentUser?.id && <div className="muted small">bieżące konto</div>}</td>
              <td>{roleLabels[item.role]}</td>
              <td><span className="status-chip" data-ok={item.active ? 'true' : 'false'}>{item.active ? 'Aktywny' : 'Wyłączony'}</span></td>
              <td>{new Date(item.created_at).toLocaleString('pl-PL')}</td>
              <td>{new Date(item.updated_at).toLocaleString('pl-PL')}</td>
              <td className="actions">
                <button type="button" className="secondary" disabled={busy !== ''} onClick={() => editUser(item)}>Edytuj</button>
                <button type="button" className="secondary" disabled={busy !== ''} onClick={() => void revokeSessions(item)}>Unieważnij sesje</button>
                <button type="button" className="danger" disabled={busy !== '' || item.id === currentUser?.id || (item.role === 'admin' && item.active && activeAdmins <= 1)} onClick={() => void removeUser(item)}>Usuń</button>
              </td>
            </tr>)}
            {users.length === 0 && <tr><td colSpan={6} className="muted">Brak użytkowników.</td></tr>}
          </tbody>
        </table>
      </div>
    </section>

    {editing && <section className="panel stack">
      <div className="section-heading">
        <div>
          <h2>Edytuj: {editing.username}</h2>
          <p className="muted">{editingSelf ? 'To jest aktualnie zalogowane konto.' : `ID: ${editing.id}`}</p>
        </div>
        <button type="button" className="secondary" onClick={() => setEditing(null)}>Zamknij</button>
      </div>

      <form className="form-grid" onSubmit={saveAccess}>
        <label>Rola
          <select disabled={editingSelf || busy !== ''} value={editingRole} onChange={(event) => setEditingRole(event.target.value as Role)}>
            <option value="viewer">Viewer</option>
            <option value="operator">Operator</option>
            <option value="admin">Administrator</option>
          </select>
        </label>
        <label className="checkbox">
          <input type="checkbox" disabled={editingSelf || busy !== '' || editingLastAdmin} checked={editingActive} onChange={(event) => setEditingActive(event.target.checked)} />
          Konto aktywne
        </label>
        <div className="form-actions">
          <button type="submit" disabled={busy !== '' || editingSelf}>{busy === 'access' ? 'Zapisywanie…' : 'Zapisz rolę i status'}</button>
        </div>
      </form>

      {editingLastAdmin && <div className="warning-banner">Nie można wyłączyć ani zdegradować ostatniego aktywnego administratora.</div>}

      <div className="form-grid">
        <label className="span-2">Nowe hasło
          <input type="password" minLength={12} value={newPassword} onChange={(event) => setNewPassword(event.target.value)} placeholder="Minimum 12 znaków" />
        </label>
        <div className="form-actions">
          <button type="button" disabled={busy !== '' || newPassword.length < 12} onClick={() => void changePassword(false)}>
            {busy === 'password' ? 'Zmiana hasła…' : 'Ustaw nowe hasło'}
          </button>
          <button type="button" className="secondary" disabled={busy !== ''} onClick={() => void changePassword(true)}>
            {busy === 'generate-password' ? 'Generowanie…' : 'Wygeneruj nowe hasło'}
          </button>
        </div>
        <p className="muted span-2">Po zmianie hasła wszystkie sesje tego użytkownika są unieważniane. Przy zmianie hasła bieżącego konta konieczne będzie ponowne logowanie.</p>
      </div>
    </section>}
  </>
}

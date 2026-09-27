import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'

export function AppLayout() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()

  async function handleLogout() {
    await logout()
    navigate('/login', { replace: true })
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">DevBox Universal</div>
        <nav>
          <NavLink to="/">Overview</NavLink>
          <NavLink to="/runtimes">Runtimes</NavLink>
          <NavLink to="/docker">Docker</NavLink>
          <NavLink to="/databases">Bazy danych</NavLink>
          <NavLink to="/domains">Domeny i Proxy</NavLink>
          <NavLink to="/ports">Porty</NavLink>
          <NavLink to="/jobs">Jobs</NavLink>
          {user?.role !== 'viewer' && <NavLink to="/audit">Audit</NavLink>}
        </nav>
      </aside>
      <main className="main-panel">
        <header className="topbar">
          <span>{user?.username} · {user?.role}</span>
          <button type="button" onClick={handleLogout}>Sign out</button>
        </header>
        <section className="content"><Outlet /></section>
      </main>
    </div>
  )
}

import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { ROUTES } from '../routes'
import { useTheme } from '../theme/ThemeProvider'

export function AppLayout() {
  const { user, logout } = useAuth()
  const { theme, toggle } = useTheme()
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
          <NavLink to={ROUTES.dashboard}>Dashboard</NavLink>
          <NavLink to={ROUTES.applications}>Aplikacje</NavLink>
          <NavLink to={ROUTES.logs}>Logs</NavLink>
          <NavLink to="/runtimes">Runtimes</NavLink>
          <NavLink to="/docker">Docker</NavLink>
          <NavLink to="/databases">Bazy danych</NavLink>
          <NavLink to="/domains">Domeny i Proxy</NavLink>
          <NavLink to="/ports">Porty</NavLink>
          <NavLink to={ROUTES.jobs}>Jobs</NavLink>
          {user?.role !== 'viewer' && <NavLink to={ROUTES.audit}>Audit</NavLink>}
        </nav>
      </aside>
      <main className="main-panel">
        <header className="topbar">
          <button className="secondary-button" type="button" onClick={toggle}>
            Theme: {theme === 'dark' ? 'Dark' : 'Light'}
          </button>
          <span>{user?.username} · {user?.role}</span>
          <button className="secondary-button" type="button" onClick={handleLogout}>Sign out</button>
        </header>
        <section className="content"><Outlet /></section>
      </main>
    </div>
  )
}

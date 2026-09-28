import { useEffect, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'
import { Modal } from '../components/Modal'
import { useTheme } from '../theme/ThemeProvider'
import { navigationForPath, navigationForRole, readPreference, savePreference } from './navigation'

export function AppLayout() {
  const { user, logout } = useAuth()
  const { theme, toggle } = useTheme()
  const navigate = useNavigate()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(() => readPreference('devbox-sidebar', 'expanded') === 'collapsed')
  const [mobileOpen, setMobileOpen] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [loggingOut, setLoggingOut] = useState(false)
  const [error, setError] = useState('')
  const searchInput = useRef<HTMLInputElement>(null)
  const items = navigationForRole(user?.role)
  const current = navigationForPath(location.pathname, user?.role)
  const filtered = items.filter((item) => `${item.label} ${item.group}`.toLocaleLowerCase('pl').includes(query.trim().toLocaleLowerCase('pl')))
  const role = user?.role === 'admin' ? 'Administrator' : user?.role === 'operator' ? 'Operator' : 'Podgląd'

  useEffect(() => {
    setMobileOpen(false)
    setSearchOpen(false)
    document.title = `${navigationForPath(location.pathname, user?.role)?.label ?? 'Aplikacja'} · DevBox Universal`
  }, [location.pathname, user?.role])

  useEffect(() => {
    function shortcut(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setMobileOpen(false)
        setQuery('')
        setSearchOpen((value) => !value)
      }
    }
    window.addEventListener('keydown', shortcut)
    return () => window.removeEventListener('keydown', shortcut)
  }, [])

  useEffect(() => {
    const desktop = window.matchMedia('(min-width: 901px)')
    const closeDrawer = () => { if (desktop.matches) setMobileOpen(false) }
    desktop.addEventListener('change', closeDrawer)
    return () => desktop.removeEventListener('change', closeDrawer)
  }, [])

  async function handleLogout() {
    setLoggingOut(true)
    setError('')
    try { await logout(); navigate('/login', { replace: true }) }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Nie udało się wylogować. Spróbuj ponownie.') }
    finally { setLoggingOut(false) }
  }

  function navigation(mobile = false) {
    return <>
      <Link to="/" className="workspace-brand" aria-label="DevBox Universal — przegląd" onClick={() => setMobileOpen(false)}>
        <span className="brand-mark"><Icon name="box" size={25} /></span>
        <span className="brand-copy">DevBox<span>UNIVERSAL</span></span>
      </Link>
      {mobile && <button className="icon-button drawer-close" aria-label="Zamknij menu" onClick={() => setMobileOpen(false)}><Icon name="close" /></button>}
      <nav className="workspace-nav" aria-label={mobile ? 'Nawigacja mobilna' : 'Nawigacja główna'}>
        {Array.from(new Set(items.map((item) => item.group))).map((group) => <div className="nav-group" key={group}>
          <div className="nav-group-label">{group}</div>
          {items.filter((item) => item.group === group).map((item) => <NavLink to={item.to} end={item.to === '/'} key={item.to}
            title={item.label} aria-label={item.label} onClick={() => setMobileOpen(false)}>
            <Icon name={item.icon} size={19} /><span className="nav-label">{item.label}</span>
          </NavLink>)}
        </div>)}
      </nav>
      <div className="sidebar-footer">
        <span className="workspace-avatar">{user?.username.slice(0, 2).toLocaleUpperCase('pl')}</span>
        <div className="nav-label user-caption"><strong>{user?.username}</strong><span>{role}</span></div>
        {!mobile && <button className="icon-button collapse-button" title={collapsed ? 'Rozwiń menu' : 'Zwiń menu'} aria-label={collapsed ? 'Rozwiń menu' : 'Zwiń menu'} aria-pressed={collapsed}
          onClick={() => setCollapsed((value) => { savePreference('devbox-sidebar', value ? 'expanded' : 'collapsed'); return !value })}><Icon name="panel" size={18} /></button>}
      </div>
    </>
  }

  return <div className={`app-shell${collapsed ? ' is-collapsed' : ''}`}>
    <a href="#main-content" className="skip-link">Przejdź do treści</a>
    <aside className="sidebar workspace-sidebar">{navigation()}</aside>
    <main className="main-panel">
      <header className="topbar workspace-topbar">
        <button className="icon-button mobile-menu-button" aria-label="Otwórz menu" aria-expanded={mobileOpen} onClick={() => setMobileOpen(true)}><Icon name="menu" /></button>
        <div className="breadcrumbs"><span>Workspace</span><Icon name="chevron" size={13} /><strong>{current?.label ?? 'Aplikacja'}</strong></div>
        <button className="workspace-search" onClick={() => { setQuery(''); setSearchOpen(true) }} aria-label="Wyszukaj widok" aria-keyshortcuts="Control+k Meta+k">
          <Icon name="search" size={17} /><span>Przejdź do widoku…</span><kbd>Ctrl K</kbd>
        </button>
        <div className="topbar-actions">
          <button className="icon-button" type="button" onClick={toggle} title={theme === 'dark' ? 'Włącz jasny motyw' : 'Włącz ciemny motyw'} aria-label={theme === 'dark' ? 'Włącz jasny motyw' : 'Włącz ciemny motyw'}><Icon name={theme === 'dark' ? 'sun' : 'moon'} /></button>
          <span className="topbar-divider" />
          <span className="topbar-user">{user?.username}</span>
          <button className="icon-button" type="button" disabled={loggingOut} title="Wyloguj się" aria-label="Wyloguj się" onClick={() => void handleLogout()}><Icon name="logout" /></button>
        </div>
      </header>
      <section id="main-content" className="content" tabIndex={-1}>
        {error && <div role="alert" className="error-banner">{error}</div>}
        <Outlet />
      </section>
    </main>
    <Modal open={mobileOpen} onClose={() => setMobileOpen(false)} labelId="mobile-menu-title" className="mobile-navigation">
      <h2 id="mobile-menu-title" className="sr-only">Menu DevBox</h2>
      <div className="workspace-sidebar">{navigation(true)}</div>
    </Modal>
    <Modal open={searchOpen} onClose={() => setSearchOpen(false)} labelId="search-title" className="command-dialog">
      <div className="modal-heading"><h2 id="search-title">Przejdź do widoku</h2><button className="icon-button" aria-label="Zamknij wyszukiwanie" onClick={() => setSearchOpen(false)}><Icon name="close" /></button></div>
      <label className="search-field"><Icon name="search" /><input ref={searchInput} autoFocus aria-label="Szukaj w nawigacji" placeholder="Aplikacje, Docker, logi…" value={query} onChange={(event) => setQuery(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && filtered[0]) { event.preventDefault(); navigate(filtered[0].to); setSearchOpen(false) }
          if (event.key === 'ArrowDown') { event.preventDefault(); document.querySelector<HTMLAnchorElement>('.command-results a')?.focus() }
        }} /></label>
      <nav className="command-results" aria-label="Wyniki wyszukiwania">
        {filtered.map((item, index) => <Link to={item.to} key={item.to} onClick={() => setSearchOpen(false)}
          onKeyDown={(event) => {
            if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
            event.preventDefault()
            const links = event.currentTarget.parentElement?.querySelectorAll<HTMLAnchorElement>('a')
            const next = index + (event.key === 'ArrowDown' ? 1 : -1)
            if (next < 0) searchInput.current?.focus()
            else links?.[Math.min(next, filtered.length - 1)]?.focus()
          }}><Icon name={item.icon} /><span>{item.label}<small>{item.group}</small></span><Icon name="arrow" size={16} /></Link>)}
        {filtered.length === 0 && <p className="empty-hint" role="status">Brak pasujących widoków. Spróbuj innej nazwy.</p>}
      </nav>
      <div className="command-footer"><span><kbd>↑</kbd> <kbd>↓</kbd> nawigacja · <kbd>Enter</kbd> otwórz</span><span><kbd>Esc</kbd> zamknij</span></div>
    </Modal>
  </div>
}

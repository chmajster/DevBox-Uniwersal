import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { DEVBOX_LOGO } from '../assets/devboxBrand'
import { Icon } from '../components/Icon'
import { useTheme } from '../theme/ThemeProvider'

export function LoginPage() {
  const { user, login } = useAuth()
  const { theme, toggle } = useTheme()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  if (user) return <Navigate to="/" replace />

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (busy) return
    setBusy(true); setError('')
    try { await login(username, password); navigate('/', { replace: true }) }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Nie udało się zalogować. Spróbuj ponownie.') }
    finally { setBusy(false) }
  }

  return <main className="login-page workspace-login">
    <section className="login-story" aria-label="DevBox Universal">
      <div className="workspace-brand"><span className="brand-mark"><img className="brand-logo" src={DEVBOX_LOGO} alt="" /></span><span className="brand-copy">DevBox<span>UNIVERSAL</span></span></div>
      <div className="login-story-body"><span className="login-kicker">TWOJA PRZESTRZEŃ DO WDRAŻANIA</span><h1>Mniej przełączania.<br />Więcej kontroli.</h1><p>Aplikacje, kontenery i usługi.<br />Jedno miejsce do zarządzania Twoim środowiskiem.</p>
        <div className="login-capabilities">{([{ icon: 'code', title: 'Aplikacje', detail: 'Git, katalogi lokalne i wdrożenia' }, { icon: 'box', title: 'Infrastruktura', detail: 'Runtime, Docker, bazy i domeny' }, { icon: 'activity', title: 'Operacje', detail: 'Zadania, monitoring i logi' }] as const).map((item) => <div key={item.title}><span><Icon name={item.icon} size={22} /></span><div><strong>{item.title}</strong><small>{item.detail}</small></div><Icon name="arrow" size={17} /></div>)}</div>
      </div><div className="login-story-footer"><Icon name="lock" size={15} />DevBox Universal · panel zarządzania</div>
    </section>
    <section className="login-form-panel" aria-labelledby="login-title">
      <button className="icon-button login-theme" onClick={toggle} aria-label={theme === 'dark' ? 'Włącz jasny motyw' : 'Włącz ciemny motyw'}><Icon name={theme === 'dark' ? 'sun' : 'moon'} /></button>
      <form className="login-card workspace-login-card" onSubmit={submit}>
        <span className="empty-icon"><Icon name="lock" size={25} /></span><div><span className="eyebrow">WITAJ PONOWNIE</span><h2 id="login-title">Zaloguj się do DevBox</h2><p>Twoje środowisko jest o krok dalej.</p></div>
        <label>Nazwa użytkownika<input autoComplete="username" placeholder="Wpisz nazwę użytkownika" value={username} onChange={(event) => setUsername(event.target.value)} required disabled={busy} /></label>
        <label>Hasło<span className="password-field"><input type={showPassword ? 'text' : 'password'} autoComplete="current-password" placeholder="Wpisz hasło" value={password} onChange={(event) => setPassword(event.target.value)} required disabled={busy} /><button className="icon-button" type="button" aria-label={showPassword ? 'Ukryj hasło' : 'Pokaż hasło'} aria-pressed={showPassword} onClick={() => setShowPassword((value) => !value)}><Icon name="eye" size={19} /></button></span></label>
        {error && <div className="error-banner" role="alert">{error}</div>}
        <button className="login-submit" type="submit" disabled={busy}>{busy ? 'Logowanie…' : 'Zaloguj się'}<Icon name="arrow" size={18} /></button>
        <p className="login-note"><Icon name="shield" size={15} />Dostęp dla uprawnionych użytkowników środowiska.</p>
      </form><span className="login-copyright">DevBox Universal</span>
    </section>
  </main>
}

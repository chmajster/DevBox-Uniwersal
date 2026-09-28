import { useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { Job, ScriptApp, ScriptAppCreateResult, ScriptAppLogs } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { StatusBadge } from '../components/StatusBadge'

const emptyForm = {
  name: '', description: '', install_source: '', update_source: '', uninstall_source: '',
  interpreter: 'bash', checksum_sha256: '', run_as_root: false, allow_insecure: false,
  service_name: '', install_now: true,
}

export function ScriptAppsPage() {
  const { user } = useAuth()
  const [items, setItems] = useState<ScriptApp[]>([])
  const [form, setForm] = useState(emptyForm)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [logs, setLogs] = useState<{ name: string; text: string } | null>(null)
  const isAdmin = user?.role === 'admin'

  async function load() {
    try {
      const data = await request<ScriptApp[]>('/script-apps')
      setItems(data ?? [])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać aplikacji instalowanych ze skryptu.')
    }
  }
  useEffect(() => { void load() }, [])

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy('create'); setError(''); setNotice('')
    try {
      const result = await request<ScriptAppCreateResult>('/script-apps', { method: 'POST', body: JSON.stringify(form) })
      setForm(emptyForm)
      setNotice(result.job ? 'Aplikacja została dodana, a instalację umieszczono w kolejce zadań.' : 'Aplikacja została dodana.')
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się dodać aplikacji.')
    } finally { setBusy('') }
  }

  async function action(item: ScriptApp, name: 'install'|'update'|'uninstall'|'start'|'stop'|'restart') {
    setBusy(item.id + ':' + name); setError(''); setNotice('')
    try {
      const job = await request<Job>(`/script-apps/${encodeURIComponent(item.id)}/${name}`, { method: 'POST' })
      setNotice(`Operację „${name}” dodano do kolejki. Job: ${job.id.slice(0, 8)}.`)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Operacja nie powiodła się.')
    } finally { setBusy('') }
  }

  async function refresh(item: ScriptApp) {
    setBusy(item.id + ':refresh'); setError('')
    try {
      await request<ScriptApp>(`/script-apps/${encodeURIComponent(item.id)}/refresh`, { method: 'POST' })
      await load()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Nie udało się odświeżyć statusu.') }
    finally { setBusy('') }
  }

  async function showLogs(item: ScriptApp) {
    setBusy(item.id + ':logs'); setError('')
    try {
      const result = await request<ScriptAppLogs>(`/script-apps/${encodeURIComponent(item.id)}/logs`)
      setLogs({ name: item.name, text: result.logs })
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać logów.') }
    finally { setBusy('') }
  }

  async function remove(item: ScriptApp) {
    if (!window.confirm(`Usunąć wpis „${item.name}”? Najpierw aplikacja musi być odinstalowana.`)) return
    setBusy(item.id + ':delete'); setError('')
    try { await request(`/script-apps/${encodeURIComponent(item.id)}`, { method: 'DELETE' }); await load() }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Nie udało się usunąć wpisu.') }
    finally { setBusy('') }
  }

  return <>
    <div className="page-heading">
      <div><h1>Aplikacje z instalatora URL / curl</h1><p className="muted">Pobieranie skryptu, kolejka zadań, status, logi oraz sterowanie wykrytą usługą systemd lub kontenerem Docker.</p></div>
      <div className="heading-actions"><Link className="secondary-button" to="/apps">Aplikacje</Link><Link className="secondary-button" to="/jobs">Zadania</Link></div>
    </div>
    {error && <div className="error-banner" role="alert">{error}</div>}
    {notice && <div className="success-banner" role="status">{notice} <Link to="/jobs">Pokaż zadania</Link></div>}

    {isAdmin && <form className="panel form-grid" onSubmit={submit}>
      <label>Nazwa<input required value={form.name} onChange={e=>setForm({...form,name:e.target.value})}/></label>
      <label>Interpreter<select value={form.interpreter} onChange={e=>setForm({...form,interpreter:e.target.value})}><option value="bash">bash</option><option value="sh">sh</option><option value="pwsh">pwsh</option><option value="powershell">powershell</option></select></label>
      <label className="span-2">URL instalatora lub komenda curl<input required placeholder="https://example.com/install.sh lub curl -fsSL https://example.com/install.sh | bash" value={form.install_source} onChange={e=>setForm({...form,install_source:e.target.value})}/></label>
      <label className="span-2">URL aktualizacji<input placeholder="opcjonalnie" value={form.update_source} onChange={e=>setForm({...form,update_source:e.target.value})}/></label>
      <label className="span-2">URL deinstalatora<input placeholder="opcjonalnie" value={form.uninstall_source} onChange={e=>setForm({...form,uninstall_source:e.target.value})}/></label>
      <label className="span-2">Opis<textarea value={form.description} onChange={e=>setForm({...form,description:e.target.value})}/></label>
      <label>SHA-256 instalatora<input placeholder="opcjonalnie, 64 znaki hex" value={form.checksum_sha256} onChange={e=>setForm({...form,checksum_sha256:e.target.value})}/></label>
      <label>Nazwa usługi systemd<input placeholder="opcjonalnie, np. coolify.service" value={form.service_name} onChange={e=>setForm({...form,service_name:e.target.value})}/></label>
      <label className="checkbox"><input type="checkbox" checked={form.run_as_root} onChange={e=>setForm({...form,run_as_root:e.target.checked})}/> Uruchamiaj przez sudo -n</label>
      <label className="checkbox"><input type="checkbox" checked={form.allow_insecure} onChange={e=>setForm({...form,allow_insecure:e.target.checked})}/> Zezwól na HTTP bez TLS</label>
      <label className="checkbox"><input type="checkbox" checked={form.install_now} onChange={e=>setForm({...form,install_now:e.target.checked})}/> Zainstaluj od razu</label>
      <div className="span-2"><button type="submit" disabled={!!busy}>{busy==='create'?'Dodawanie…':'Dodaj aplikację'}</button></div>
      <p className="span-2 muted small">DevBox nie wykonuje wklejonego pipeline’u przez eval. Z komendy curl pobierany jest wyłącznie URL, skrypt trafia do pliku tymczasowego i jest uruchamiany wybranym interpreterem.</p>
    </form>}

    <div className="table-wrap">
      <table><thead><tr><th>Nazwa</th><th>Status</th><th>Manager</th><th>Target</th><th>Installer</th><th>Akcje</th></tr></thead>
      <tbody>{items.map(item=><tr key={item.id}>
        <td><strong>{item.name}</strong><div className="muted small">{item.description}</div></td>
        <td><StatusBadge status={item.status}/>{item.last_error&&<div className="error-text small">{item.last_error}</div>}</td>
        <td>{item.manager}</td><td><code>{item.manager_target||'—'}</code></td>
        <td className="clip-cell" title={item.install_source}>{item.install_source}</td>
        <td><div className="project-actions">
          <button className="secondary-button button-compact" disabled={!!busy} onClick={()=>void refresh(item)}>Status</button>
          <button className="secondary-button button-compact" disabled={!!busy} onClick={()=>void showLogs(item)}>Logi</button>
          {isAdmin&&<>
            {(item.status==='not_installed'||item.status==='uninstalled'||item.status==='failed')&&<button className="button-compact" disabled={!!busy} onClick={()=>void action(item,'install')}>Install</button>}
            {item.update_source&&<button className="secondary-button button-compact" disabled={!!busy} onClick={()=>void action(item,'update')}>Update</button>}
            {item.manager!=='script'&&<><button className="secondary-button button-compact" disabled={!!busy} onClick={()=>void action(item,'start')}>Start</button><button className="secondary-button button-compact" disabled={!!busy} onClick={()=>void action(item,'stop')}>Stop</button><button className="secondary-button button-compact" disabled={!!busy} onClick={()=>void action(item,'restart')}>Restart</button></>}
            {item.uninstall_source&&<button className="danger button-compact" disabled={!!busy} onClick={()=>void action(item,'uninstall')}>Uninstall</button>}
            {(item.status==='not_installed'||item.status==='uninstalled')&&<button className="icon-button" disabled={!!busy} onClick={()=>void remove(item)}>Usuń</button>}
          </>}
        </div></td>
      </tr>)}
      {!items.length&&<tr><td colSpan={6} className="muted">Brak aplikacji instalowanych ze skryptu.</td></tr>}</tbody></table>
    </div>

    {logs&&<div className="panel"><div className="page-heading"><div><h2>Logi: {logs.name}</h2></div><button className="secondary-button" onClick={()=>setLogs(null)}>Zamknij</button></div><pre className="log-output">{logs.text||'Brak logów.'}</pre></div>}
  </>
}

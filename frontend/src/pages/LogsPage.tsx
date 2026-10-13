import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { Application } from '../applications/model'
import { request, apiURL } from '../api/client'
import { listLogSources, listLogs, logQuery } from '../api/operations'
import type { LogEntry } from '../api/types'
import { ErrorState } from '../components/ErrorState'

const requiredSources = ['all', 'devbox', 'project', 'deployment', 'job', 'docker']
export function LogsPage() {
  const [params, setParams] = useSearchParams()
  const source = params.get('source') || 'all'
  const [project, setProject] = useState('')
  const [level, setLevel] = useState('')
  const [search, setSearch] = useState('')
  const [since, setSince] = useState('')
  const [until, setUntil] = useState('')
  const [live, setLive] = useState(false)
  const [sources, setSources] = useState<string[]>(requiredSources)
  const [projects, setProjects] = useState<Application[]>([])
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [error, setError] = useState('')
  const cursorRef = useRef(0)
  useEffect(() => {
    let cancelled = false
    void listLogSources().then((available) => { if (!cancelled) setSources(Array.from(new Set([...requiredSources, ...(available ?? [])]))) }).catch(() => { if (!cancelled) setSources(requiredSources) })
    void request<Application[]>('/applications').then((items) => { if (!cancelled) setProjects(items) }).catch(() => { if (!cancelled) setProjects([]) })
    return () => { cancelled = true }
  }, [])
  const filters = useMemo(() => ({ source, project: project || undefined, level: level || undefined, search: search || undefined,
    since: since ? new Date(since).toISOString() : undefined, until: until ? new Date(until).toISOString() : undefined, limit: 300 }), [source, project, level, search, since, until])
  useEffect(() => {
    let cancelled = false
    setError(''); setLogs([])
    listLogs(filters).then((items) => {
      if (cancelled) return
      const entries = items ?? []
      setLogs(entries); cursorRef.current = entries.length ? entries[entries.length - 1].cursor : 0
    }).catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { cancelled = true }
  }, [filters])
  useEffect(() => {
    if (!live) return
    const stream = new EventSource(apiURL(`/logs/stream?${logQuery({ ...filters, after: cursorRef.current || undefined })}`), { withCredentials: true })
    const onLog = (event: MessageEvent<string>) => {
      try {
        const entry = JSON.parse(event.data) as LogEntry
        cursorRef.current = Math.max(cursorRef.current, entry.cursor)
        setLogs((current) => [...current.filter((item) => item.id !== entry.id), entry].slice(-500))
      } catch { setError('Strumień logów zwrócił nieprawidłowy wpis.') }
    }
    stream.addEventListener('log', onLog as EventListener)
    stream.addEventListener('error', () => setError(`Strumień logów dla źródła „${source}” został rozłączony.`))
    return () => stream.close()
  }, [live, filters, source])
  async function exportLogs() {
    try {
      const response = await fetch(apiURL(`/logs/export?${logQuery(filters)}`), { credentials: 'include' })
      if (!response.ok) throw new Error(`Eksport logów: HTTP ${response.status}`)
      const href = URL.createObjectURL(await response.blob())
      const anchor = document.createElement('a'); anchor.href = href; anchor.download = 'devbox-logs.log'; anchor.click()
      window.setTimeout(() => URL.revokeObjectURL(href), 1000)
    } catch (reason) { setError(reason instanceof Error ? reason.message : 'Eksport nie powiódł się.') }
  }
  return <>
    <div className="page-heading"><div><h1>Logi</h1><p className="muted">Centralny podgląd zdarzeń aplikacji, zadań i usług.</p></div></div>
    <div className="filter-bar">
      <label>Źródło<select value={source} onChange={(event) => { const next = new URLSearchParams(params); next.set('source', event.target.value); setParams(next, { replace: true }) }}>{sources.map((item) => <option key={item} value={item}>{item === 'all' ? 'Wszystkie usługi' : item}</option>)}</select></label>
      <label>Aplikacja<select value={project} onChange={(event) => setProject(event.target.value)}><option value="">Wszystkie</option>{projects.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      <label>Poziom<select value={level} onChange={(event) => setLevel(event.target.value)}><option value="">Wszystkie</option>{['debug', 'info', 'warn', 'error'].map((value) => <option key={value} value={value}>{value.toUpperCase()}</option>)}</select></label>
      <label className="search-filter">Wyszukiwanie<input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Treść komunikatu" /></label>
      <label>Od<input type="datetime-local" value={since} onChange={(event) => setSince(event.target.value)} /></label><label>Do<input type="datetime-local" value={until} onChange={(event) => setUntil(event.target.value)} /></label>
      <label className="live-toggle"><input type="checkbox" checked={live} onChange={(event) => setLive(event.target.checked)} />Na żywo</label><button className="secondary" onClick={() => void exportLogs()}>Eksportuj</button>
    </div>
    {error && <ErrorState message={error} />}
    <div className="log-console log-console-large">{logs.length === 0 && !error ? <div className="muted">Brak wpisów dla wybranych filtrów.</div> : logs.map((entry) => <div className="log-line" key={entry.id}><time>{new Date(entry.created_at).toLocaleString('pl-PL')}</time><span className="log-source">{entry.source}</span><strong>{entry.level.toUpperCase()}</strong><span>{entry.message}</span></div>)}</div>
  </>
}

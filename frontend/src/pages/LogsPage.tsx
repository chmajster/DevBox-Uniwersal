import { useEffect, useMemo, useRef, useState } from 'react'
import { apiURL } from '../api/client'
import { listLogSources, listLogs, listProjects, logQuery } from '../api/operations'
import type { LogEntry, Project } from '../api/types'
import { ErrorState } from '../components/ErrorState'

const requiredSources = ['devbox', 'project', 'deployment', 'docker', 'nginx']

export function LogsPage() {
  const [source, setSource] = useState('devbox')
  const [project, setProject] = useState('')
  const [level, setLevel] = useState('')
  const [search, setSearch] = useState('')
  const [live, setLive] = useState(false)
  const [sources, setSources] = useState<string[]>(requiredSources)
  const [projects, setProjects] = useState<Project[]>([])
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [error, setError] = useState('')
  const cursorRef = useRef(0)

  useEffect(() => {
    void listLogSources()
      .then((available) => setSources(Array.from(new Set([...requiredSources, ...available]))))
      .catch(() => setSources(requiredSources))
    void listProjects().then(setProjects).catch(() => setProjects([]))
  }, [])

  const filters = useMemo(() => ({
    source,
    project: project || undefined,
    level: level || undefined,
    search: search || undefined,
    limit: 300
  }), [source, project, level, search])

  useEffect(() => {
    let cancelled = false
    setError('')
    listLogs(filters)
      .then((items) => {
        if (cancelled) return
        setLogs(items)
        cursorRef.current = items.length > 0 ? items[items.length - 1].cursor : 0
      })
      .catch((reason: unknown) => { if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason)) })
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
      } catch {
        setError('Log stream returned an invalid event payload')
      }
    }
    const onError = () => setError(`Live log stream disconnected for source "${source}"`)
    stream.addEventListener('log', onLog as EventListener)
    stream.addEventListener('error', onError)
    return () => stream.close()
  }, [live, filters, source])

  return <>
    <div className="page-heading">
      <div>
        <h1>Logs</h1>
        <p className="muted">Central operational log viewer. Provider-owned sources are consumed through the log-source API.</p>
      </div>
    </div>

    <div className="filter-bar">
      <label>Source<select value={source} onChange={(event) => setSource(event.target.value)}>
        {sources.map((item) => <option key={item} value={item}>{item}</option>)}
      </select></label>
      <label>Project<select value={project} onChange={(event) => setProject(event.target.value)}>
        <option value="">All</option>
        {projects.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </select></label>
      <label>Level<select value={level} onChange={(event) => setLevel(event.target.value)}>
        <option value="">All</option>
        <option value="debug">DEBUG</option>
        <option value="info">INFO</option>
        <option value="warn">WARN</option>
        <option value="error">ERROR</option>
      </select></label>
      <label className="search-filter">Search<input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Message text" /></label>
      <label className="live-toggle"><input type="checkbox" checked={live} onChange={(event) => setLive(event.target.checked)} /> Live tail</label>
    </div>

    {error && <ErrorState message={error} />}
    <div className="log-console log-console-large">
      {logs.length === 0 && !error
        ? <div className="muted">No log entries match the current filters.</div>
        : logs.map((entry) => <div className="log-line" key={entry.id}>
            <time>{new Date(entry.created_at).toLocaleString()}</time>
            <span className="log-source">{entry.source}</span>
            <strong>{entry.level.toUpperCase()}</strong>
            <span>{entry.message}</span>
          </div>)}
    </div>
  </>
}

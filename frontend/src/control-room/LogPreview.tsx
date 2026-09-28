import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { LogEntry } from '../api/types'
import { Icon } from '../components/Icon'
import { collection } from './api'
import { time } from './model'
import { usePolling } from './usePolling'

export function LogPreview() {
  const [source, setSource] = useState('all')
  const [limit, setLimit] = useState(100)
  const [sources, setSources] = useState<string[]>(['all'])
  useEffect(() => {
    const controller = new AbortController()
    request<string[] | null>('/logs/sources', { signal: controller.signal }).then((items) => { if (!controller.signal.aborted) setSources([...new Set(['all', ...(items ?? [])])]) }).catch(() => { /* Aggregate source still has its own visible error state. */ })
    return () => controller.abort()
  }, [])
  const load = useCallback(async (signal: AbortSignal) => collection(await request<LogEntry[] | null>(`/logs?${new URLSearchParams({ source, limit: String(limit) })}`, { signal })), [source, limit])
  const logs = usePolling(load, 5000)
  const selected = (logs.data ?? []).filter((entry) => source === 'all' || entry.source === source).slice(-limit)
  return <section className="console-panel log-preview">
    <div className="console-panel-heading"><h2><Icon name="logs" size={18} />Ostatnie logi</h2><Link to={`/logs?${new URLSearchParams({ source })}`}>Pełny widok <Icon name="arrow" size={14} /></Link></div>
    <div className="log-preview-toolbar"><label><span className="sr-only">Źródło logów</span><select value={source} onChange={(event) => setSource(event.target.value)}>{sources.map((value) => <option key={value} value={value}>{value === 'all' ? 'Wszystkie usługi' : value}</option>)}</select></label>
      <label><span className="sr-only">Liczba linii</span><select value={limit} onChange={(event) => setLimit(Number(event.target.value))}>{[20, 50, 100].map((value) => <option key={value} value={value}>Ostatnie {value} linii</option>)}</select></label><span>Odczyt co 5 s</span></div>
    {logs.error && <p className="error-banner" role="alert">{logs.error}</p>}
    <div className="console-log-lines" role="region" aria-label="Ostatnie wpisy logów" tabIndex={0} aria-busy={logs.loading}>
      {selected.length === 0 && <p className="console-empty">{logs.loading ? 'Pobieranie logów…' : logs.error ? 'Logi niedostępne.' : 'Brak wpisów dla wybranego źródła.'}</p>}
      {selected.map((entry) => <div className={`console-log-line log-${['warn', 'error', 'debug', 'info'].includes(entry.level.toLowerCase()) ? entry.level.toLowerCase() : 'info'}`} key={entry.id}>
        <time dateTime={entry.created_at}>{time(entry.created_at)}</time><span>{entry.source}</span><strong>[{entry.level}]</strong><code>{entry.message}</code>
      </div>)}
    </div>
  </section>
}

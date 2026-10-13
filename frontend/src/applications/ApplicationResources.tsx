import { useCallback } from 'react'
import { request } from '../api/client'
import { usePolling } from '../control-room/usePolling'
import { Icon } from '../components/Icon'

interface Resource { ID: string; Name: string; CPUPerc: string; MemUsage: string; MemPerc: string; NetIO: string }
export function ApplicationResources({ id }: { id: string }) {
  const load = useCallback((signal: AbortSignal) => request<Resource[]>(`/applications/${encodeURIComponent(id)}/stats`, { signal }), [id])
  const stats = usePolling(load, 10000)
  return <section className="acp-card"><div className="app-section-title"><Icon name="activity" /><h2>Zużycie zasobów</h2><span className="app-subtle-label">Pomiar co 10 s</span></div>
    {stats.error && <p className="error-banner" role="alert">{stats.error}</p>}
    {!stats.error && !stats.data && <p className="muted" role="status">Wczytywanie pomiarów z Dockera…</p>}
    {(stats.data ?? []).map((item) => {
      const [memory, limit] = item.MemUsage.split('/').map((part) => part.trim())
      const [received, sent] = item.NetIO.split('/').map((part) => part.trim())
      const memoryPercent = Number(item.MemPerc.replace('%', ''))
      return <div key={item.ID}><div className="app-resource-grid">
        <div className="app-resource"><span className="app-resource-label"><Icon name="cpu" size={17} />Procesor</span><strong>{item.CPUPerc || '—'}</strong><small>Użycie CPU kontenera</small></div>
        <div className="app-resource"><span className="app-resource-label"><Icon name="memory" size={17} />Pamięć RAM</span><strong>{memory || '—'}</strong><small>{limit ? `Limit ${limit} · ${item.MemPerc}` : 'Brak informacji o limicie'}</small>{item.MemPerc && Number.isFinite(memoryPercent) && memoryPercent >= 0 && memoryPercent <= 100 && <progress value={memoryPercent} max={100} aria-label={`Użycie pamięci ${item.Name}`} />}</div>
        <div className="app-resource"><span className="app-resource-label"><Icon name="network" size={17} />Transfer sieciowy</span><strong>{received || '—'}</strong><small>Odebrano{sent ? ` · wysłano ${sent}` : ''}</small></div>
      </div><p className="app-resource-caption">Kontener <code>{item.Name}</code></p></div>
    })}
    {stats.data?.length === 0 && <div className="app-empty-inline"><Icon name="activity" size={24} /><p>Brak pomiarów. Zasoby pojawią się po uruchomieniu kontenera.</p></div>}
  </section>
}

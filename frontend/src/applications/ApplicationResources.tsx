import { useCallback } from 'react'
import { request } from '../api/client'
import { usePolling } from '../control-room/usePolling'

interface Resource { ID: string; Name: string; CPUPerc: string; MemUsage: string; MemPerc: string; NetIO: string }
export function ApplicationResources({ id }: { id: string }) {
  const load = useCallback((signal: AbortSignal) => request<Resource[]>(`/applications/${encodeURIComponent(id)}/stats`, { signal }), [id])
  const stats = usePolling(load, 10000)
  return <section className="acp-card"><h2>CPU i pamięć</h2>{stats.error && <p role="alert">{stats.error}</p>}<div className="acp-table"><table><thead><tr><th>Kontener</th><th>CPU</th><th>RAM / limit</th><th>RAM %</th><th>Sieć</th></tr></thead><tbody>{(stats.data ?? []).map((item) => <tr key={item.ID}><td>{item.Name}</td><td>{item.CPUPerc}</td><td>{item.MemUsage}</td><td>{item.MemPerc}</td><td>{item.NetIO}</td></tr>)}{!stats.data?.length && <tr><td colSpan={5}>Brak działających kontenerów.</td></tr>}</tbody></table></div></section>
}

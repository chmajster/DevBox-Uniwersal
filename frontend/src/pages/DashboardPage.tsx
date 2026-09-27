import { useEffect, useState } from 'react'
import { request } from '../api/client'
import type { SystemInfo } from '../api/types'

export function DashboardPage() {
  const [info, setInfo] = useState<SystemInfo | null>(null)
  const [error, setError] = useState('')
  useEffect(() => { request<SystemInfo>('/system/info').then(setInfo).catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load system info')) }, [])
  return <>
    <h1>Overview</h1>
    <p className="muted">Foundation status for the local DevBox control plane.</p>
    {error && <div className="error-banner">{error}</div>}
    {info && <div className="cards">
      <article><span>Host</span><strong>{info.hostname}</strong></article>
      <article><span>Platform</span><strong>{info.os}/{info.arch}</strong></article>
      <article><span>Backend</span><strong>{info.version}</strong></article>
    </div>}
  </>
}

import { useEffect, useState } from 'react'
import { request } from '../api/client'
import type { AuditEvent } from '../api/types'

export function AuditPage() {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [error, setError] = useState('')
  useEffect(() => { request<AuditEvent[]>('/audit').then((items) => setEvents(items ?? [])).catch((e: unknown) => setError(e instanceof Error ? e.message : 'Failed to load audit')) }, [])
  return <><h1>Audit</h1>{error && <div className="error-banner">{error}</div>}
    <table><thead><tr><th>Action</th><th>Resource</th><th>Timestamp</th></tr></thead><tbody>
      {events.map((event) => <tr key={event.id}><td>{event.action}</td><td>{event.resource_type}</td><td>{new Date(event.created_at).toLocaleString()}</td></tr>)}
      {events.length === 0 && <tr><td colSpan={3} className="muted">No audit events yet.</td></tr>}
    </tbody></table></>
}

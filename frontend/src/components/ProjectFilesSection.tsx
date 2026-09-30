import { useEffect, useState } from 'react'
import { request } from '../api/client'

interface ProjectFileEntry {
  name: string
  path: string
  kind: 'directory' | 'file'
  size_bytes: number
  modified_at: string
  is_symlink?: boolean
}

interface ProjectFileListing {
  path: string
  parent?: string
  entries: ProjectFileEntry[]
  truncated?: boolean
}

interface ProjectFilesSectionProps {
  projectId: string
  rootPath: string
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let amount = value
  let unit = 0
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024
    unit += 1
  }
  return `${amount >= 10 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`
}

export function ProjectFilesSection({ projectId, rootPath }: ProjectFilesSectionProps) {
  const [listing, setListing] = useState<ProjectFileListing | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  async function browse(path: string) {
    setLoading(true)
    setError('')
    try {
      const result = await request<ProjectFileListing>(`/projects/${projectId}/files?path=${encodeURIComponent(path)}`)
      setListing(result)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać zawartości katalogu aplikacji.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    setListing(null)
    void browse('')
  }, [projectId])

  const parts = listing?.path.split('/').filter(Boolean) ?? []
  const breadcrumbs = parts.map((name, index) => ({
    name,
    path: parts.slice(0, index + 1).join('/'),
  }))
  const currentDisplayPath = listing?.path
    ? `${rootPath.replace(/[\\/]+$/, '')}/${listing.path}`
    : rootPath

  return <div className="stack">
    <section className="panel">
      <div className="section-heading">
        <div>
          <h2>Pliki aplikacji</h2>
          <p className="muted">Rzeczywista zawartość katalogu źródłowego używanego przez tę aplikację. Lista pochodzi bezpośrednio z filesystemu hosta.</p>
        </div>
        <div className="row-actions">
          {listing?.path && <button type="button" className="secondary" disabled={loading} onClick={() => void browse(listing.parent ?? '')}>Poziom wyżej</button>}
          <button type="button" className="secondary" disabled={loading} onClick={() => void browse(listing?.path ?? '')}>{loading ? 'Odświeżanie…' : 'Odśwież'}</button>
        </div>
      </div>
      <p className="muted small">Katalog aplikacji: <code>{currentDisplayPath || '—'}</code></p>
      <div className="row-actions" aria-label="Ścieżka katalogu">
        <button type="button" className="secondary" disabled={loading} onClick={() => void browse('')}>/</button>
        {breadcrumbs.map((crumb) => <button key={crumb.path} type="button" className="secondary" disabled={loading} onClick={() => void browse(crumb.path)}>{crumb.name}</button>)}
      </div>
    </section>

    {error && <div className="error-banner">{error}</div>}
    {listing?.truncated && <div className="warning-banner">Katalog zawiera więcej niż 1000 dostępnych wpisów. Wyświetlono pierwsze 1000.</div>}

    <div className="table-wrap">
      <table>
        <thead><tr><th>Nazwa</th><th>Typ</th><th>Rozmiar</th><th>Zmodyfikowano</th></tr></thead>
        <tbody>
          {listing?.entries.map((entry) => <tr key={entry.path}>
            <td>
              {entry.kind === 'directory'
                ? <span className="row-actions"><button type="button" className="secondary" disabled={loading} onClick={() => void browse(entry.path)}>{entry.name}</button></span>
                : <code>{entry.name}</code>}
            </td>
            <td>{entry.kind === 'directory' ? 'Katalog' : 'Plik'}{entry.is_symlink ? ' · symlink' : ''}</td>
            <td>{entry.kind === 'directory' ? '—' : formatBytes(entry.size_bytes)}</td>
            <td>{entry.modified_at ? new Date(entry.modified_at).toLocaleString('pl-PL') : '—'}</td>
          </tr>)}
          {!loading && listing && listing.entries.length === 0 && <tr><td colSpan={4} className="muted">Katalog jest pusty.</td></tr>}
          {loading && !listing && <tr><td colSpan={4} className="muted">Pobieranie zawartości katalogu…</td></tr>}
        </tbody>
      </table>
    </div>
  </div>
}

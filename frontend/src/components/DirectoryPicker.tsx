import { useEffect, useState } from 'react'
import { request } from '../api/client'
import './DirectoryPicker.css'

interface DirectoryEntry {
  name: string
  path: string
}

interface DirectoryListing {
  path: string
  parent?: string
  directories: DirectoryEntry[]
}

interface DirectoryPickerProps {
  value: string
  onSelect: (path: string) => void
  onClose: () => void
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

export function DirectoryPicker({ value, onSelect, onClose }: DirectoryPickerProps) {
  const [selected, setSelected] = useState(value || '/')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [listings, setListings] = useState<Record<string, DirectoryEntry[]>>({})
  const [loading, setLoading] = useState<Set<string>>(new Set())
  const [errors, setErrors] = useState<Record<string, string>>({})

  useEffect(() => {
    let cancelled = false

    async function loadRoot() {
      setLoading(current => new Set(current).add('/'))
      try {
        const listing = await request<DirectoryListing>('/projects/directories?path=%2F')
        if (cancelled) return
        setListings(current => ({ ...current, [listing.path]: listing.directories ?? [] }))
        setExpanded(current => new Set(current).add(listing.path))
        if (!value) setSelected(listing.path)
      } catch (error) {
        if (!cancelled) setErrors(current => ({ ...current, '/': errorMessage(error) }))
      } finally {
        if (!cancelled) {
          setLoading(current => {
            const next = new Set(current)
            next.delete('/')
            return next
          })
        }
      }
    }

    void loadRoot()
    return () => { cancelled = true }
  }, [value])

  async function loadDirectory(path: string) {
    setLoading(current => new Set(current).add(path))
    setErrors(current => {
      const next = { ...current }
      delete next[path]
      return next
    })
    try {
      const listing = await request<DirectoryListing>(`/projects/directories?path=${encodeURIComponent(path)}`)
      setListings(current => ({
        ...current,
        [path]: listing.directories ?? [],
        [listing.path]: listing.directories ?? []
      }))
      return path
    } catch (error) {
      setErrors(current => ({ ...current, [path]: errorMessage(error) }))
      return null
    } finally {
      setLoading(current => {
        const next = new Set(current)
        next.delete(path)
        return next
      })
    }
  }

  async function toggle(path: string) {
    if (expanded.has(path)) {
      setExpanded(current => {
        const next = new Set(current)
        next.delete(path)
        return next
      })
      return
    }

    let resolvedPath = path
    if (!Object.prototype.hasOwnProperty.call(listings, path)) {
      const loadedPath = await loadDirectory(path)
      if (!loadedPath) return
      resolvedPath = loadedPath
    }
    setExpanded(current => new Set(current).add(resolvedPath))
  }

  function renderNode(path: string, label: string, depth: number) {
    const isExpanded = expanded.has(path)
    const isLoading = loading.has(path)
    const children = listings[path] ?? []

    return <div key={path} className="directory-tree-node">
      <div
        className={`directory-tree-row${selected === path ? ' is-selected' : ''}`}
        style={{ paddingLeft: `${depth * 18}px` }}
      >
        <button
          type="button"
          className="directory-tree-toggle"
          aria-label={isExpanded ? `Zwiń ${label}` : `Rozwiń ${label}`}
          aria-expanded={isExpanded}
          onClick={() => void toggle(path)}
          disabled={isLoading}
        >
          {isLoading ? '…' : isExpanded ? '−' : '+'}
        </button>
        <button
          type="button"
          className="directory-tree-name"
          onClick={() => setSelected(path)}
          onDoubleClick={() => {
            onSelect(path)
            onClose()
          }}
          title={path}
        >
          {label}
        </button>
      </div>
      {errors[path] && <div className="directory-tree-error" style={{ paddingLeft: `${depth * 18 + 34}px` }}>{errors[path]}</div>}
      {isExpanded && children.map(child => renderNode(child.path, child.name, depth + 1))}
      {isExpanded && !isLoading && !errors[path] && children.length === 0 &&
        <div className="directory-tree-empty" style={{ paddingLeft: `${depth * 18 + 34}px` }}>Brak podkatalogów</div>}
    </div>
  }

  return <div className="directory-picker">
    <div className="directory-picker-header">
      <div>
        <strong>Wybierz katalog</strong>
        <span className="muted">Rozwijaj katalogi po stronie systemu, na którym działa DevBox.</span>
      </div>
      <button type="button" className="secondary" onClick={onClose}>Zamknij</button>
    </div>

    <div className="directory-tree" role="tree" aria-label="Drzewo katalogów">
      {renderNode('/', '/', 0)}
    </div>

    <div className="directory-picker-selection">
      <div>
        <span className="muted">Wybrana ścieżka</span>
        <code>{selected}</code>
      </div>
      <button
        type="button"
        onClick={() => {
          onSelect(selected)
          onClose()
        }}
      >
        Wybierz katalog
      </button>
    </div>
  </div>
}

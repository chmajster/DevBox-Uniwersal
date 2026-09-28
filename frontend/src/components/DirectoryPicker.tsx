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
  truncated?: boolean
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
  const [selected, setSelected] = useState(value)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [listings, setListings] = useState<Record<string, DirectoryEntry[]>>({})
  const [loading, setLoading] = useState<Set<string>>(new Set())
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [truncated, setTruncated] = useState<Set<string>>(new Set())

  useEffect(() => {
    let cancelled = false

    async function loadRoots() {
      setLoading(current => new Set(current).add(''))
      try {
        const listing = await request<DirectoryListing>('/projects/directories')
        if (cancelled) return
        const roots = listing.directories ?? []
        setListings(current => ({ ...current, '': roots }))
        setExpanded(current => new Set(current).add(''))
        if (!value && roots.length > 0) setSelected(roots[0].path)
      } catch (error) {
        if (!cancelled) setErrors(current => ({ ...current, '': errorMessage(error) }))
      } finally {
        if (!cancelled) {
          setLoading(current => {
            const next = new Set(current)
            next.delete('')
            return next
          })
        }
      }
    }

    void loadRoots()
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
      if (listing.truncated) {
        setTruncated(current => new Set(current).add(listing.path))
      }
      return listing.path
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

  function renderNode(path: string, label: string, depth: number, ancestors: Set<string>) {
    const cycle = ancestors.has(path)
    const isExpanded = expanded.has(path)
    const isLoading = loading.has(path)
    const children = listings[path] ?? []
    const nextAncestors = new Set(ancestors)
    nextAncestors.add(path)

    return <div key={`${depth}:${path}`} className="directory-tree-node">
      <div
        className={`directory-tree-row${selected === path ? ' is-selected' : ''}`}
        style={{ paddingLeft: `${depth * 18}px` }}
      >
        <button
          type="button"
          className="directory-tree-toggle"
          aria-label={cycle ? `Cykl ${label}` : isExpanded ? `Zwiń ${label}` : `Rozwiń ${label}`}
          aria-expanded={!cycle && isExpanded}
          onClick={() => { if (!cycle) void toggle(path) }}
          disabled={isLoading || cycle || depth >= 32}
        >
          {cycle ? '↺' : isLoading ? '…' : isExpanded ? '−' : '+'}
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
      {cycle && <div className="directory-tree-empty" style={{ paddingLeft: `${depth * 18 + 34}px` }}>Pominięto cykl dowiązania symbolicznego</div>}
      {errors[path] && <div className="directory-tree-error" style={{ paddingLeft: `${depth * 18 + 34}px` }}>{errors[path]}</div>}
      {!cycle && isExpanded && children.map(child => renderNode(child.path, child.name, depth + 1, nextAncestors))}
      {!cycle && isExpanded && truncated.has(path) &&
        <div className="directory-tree-empty" style={{ paddingLeft: `${depth * 18 + 34}px` }}>Lista ograniczona do 500 katalogów</div>}
      {!cycle && isExpanded && !isLoading && !errors[path] && children.length === 0 &&
        <div className="directory-tree-empty" style={{ paddingLeft: `${depth * 18 + 34}px` }}>Brak podkatalogów</div>}
    </div>
  }

  const roots = listings[''] ?? []

  return <div className="directory-picker">
    <div className="directory-picker-header">
      <div>
        <strong>Wybierz katalog</strong>
        <span className="muted">Widoczne są wyłącznie katalogi dozwolone przez konfigurację DevBox.</span>
      </div>
      <button type="button" className="secondary" onClick={onClose}>Zamknij</button>
    </div>

    <div className="directory-tree" role="tree" aria-label="Drzewo katalogów">
      {loading.has('') && <div className="directory-tree-empty">Ładowanie katalogów…</div>}
      {errors[''] && <div className="directory-tree-error">{errors['']}</div>}
      {!loading.has('') && !errors[''] && roots.map(root => renderNode(root.path, root.name, 0, new Set()))}
      {!loading.has('') && !errors[''] && roots.length === 0 && <div className="directory-tree-empty">Brak skonfigurowanych katalogów do przeglądania</div>}
    </div>

    <div className="directory-picker-selection">
      <div>
        <span className="muted">Wybrana ścieżka</span>
        <code>{selected || '—'}</code>
      </div>
      <button
        type="button"
        disabled={!selected}
        onClick={() => {
          if (!selected) return
          onSelect(selected)
          onClose()
        }}
      >
        Wybierz katalog
      </button>
    </div>
  </div>
}

import { useEffect, useRef, useState } from 'react'
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

function isAbortError(error: unknown) {
  return typeof error === 'object' && error !== null && 'name' in error && (error as { name?: string }).name === 'AbortError'
}

function normalizeForCompare(path: string) {
  const normalized = path.replace(/\\/g, '/')
  return normalized.length > 1 ? normalized.replace(/\/+$/, '') : normalized
}

function pathWithin(target: string, root: string) {
  const normalizedTarget = normalizeForCompare(target)
  const normalizedRoot = normalizeForCompare(root)
  return normalizedTarget === normalizedRoot || normalizedTarget.startsWith(`${normalizedRoot}/`)
}

function pathSegments(target: string, root: string) {
  const normalizedTarget = normalizeForCompare(target)
  const normalizedRoot = normalizeForCompare(root)
  if (!pathWithin(normalizedTarget, normalizedRoot) || normalizedTarget === normalizedRoot) return []
  return normalizedTarget.slice(normalizedRoot.length + 1).split('/').filter(Boolean)
}

function joinPath(base: string, segment: string) {
  const separator = base.includes('\\') && !base.includes('/') ? '\\' : '/'
  return base.endsWith(separator) ? `${base}${segment}` : `${base}${separator}${segment}`
}

function Chevron({ expanded, loading, cycle }: { expanded: boolean; loading: boolean; cycle: boolean }) {
  if (loading) return <span className="directory-tree-spinner" aria-hidden="true" />
  if (cycle) return <span className="directory-tree-cycle" aria-hidden="true">↻</span>
  return <span className={`directory-tree-chevron${expanded ? ' is-expanded' : ''}`} aria-hidden="true" />
}

function FolderIcon({ open }: { open: boolean }) {
  return <span className={`directory-tree-folder${open ? ' is-open' : ''}`} aria-hidden="true">
    <span className="directory-tree-folder-tab" />
  </span>
}

export function DirectoryPicker({ value, onSelect, onClose }: DirectoryPickerProps) {
  const [selected, setSelected] = useState(value)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [listings, setListings] = useState<Record<string, DirectoryEntry[]>>({})
  const [loading, setLoading] = useState<Set<string>>(new Set())
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [truncated, setTruncated] = useState<Set<string>>(new Set())
  const listingsRef = useRef<Record<string, DirectoryEntry[]>>({})
  const rootsRef = useRef<DirectoryEntry[]>([])
  const valueRef = useRef(value)
  const expansionRunRef = useRef(0)
  const expansionAbortRef = useRef<AbortController | null>(null)
  const selectedRowRef = useRef<HTMLDivElement | null>(null)

  valueRef.current = value

  function cacheListing(requestedPath: string, listing: DirectoryListing) {
    const directories = listing.directories ?? []
    const next = {
      ...listingsRef.current,
      [requestedPath]: directories,
      [listing.path]: directories,
    }
    listingsRef.current = next
    setListings(next)
    setTruncated((current) => {
      const updated = new Set(current)
      for (const path of [requestedPath, listing.path]) {
        if (listing.truncated) updated.add(path)
        else updated.delete(path)
      }
      return updated
    })
  }

  function injectChild(parentPath: string, child: DirectoryEntry) {
    const current = listingsRef.current[parentPath] ?? []
    if (current.some((entry) => entry.path === child.path)) return
    const children = [...current, child].sort((left, right) => left.name.localeCompare(right.name))
    const next = { ...listingsRef.current, [parentPath]: children }
    listingsRef.current = next
    setListings(next)
  }

  async function loadDirectory(path: string, signal?: AbortSignal, showError = true): Promise<DirectoryListing | null> {
    if (Object.prototype.hasOwnProperty.call(listingsRef.current, path)) {
      return { path, directories: listingsRef.current[path] }
    }

    setLoading((current) => new Set(current).add(path))
    if (showError) {
      setErrors((current) => {
        const next = { ...current }
        delete next[path]
        return next
      })
    }

    try {
      const listing = await request<DirectoryListing>(`/project-directories?path=${encodeURIComponent(path)}`, { signal })
      if (signal?.aborted) return null
      cacheListing(path, listing)
      return listing
    } catch (error) {
      if (!signal?.aborted && !isAbortError(error) && showError) {
        setErrors((current) => ({ ...current, [path]: errorMessage(error) }))
      }
      return null
    } finally {
      setLoading((current) => {
        const next = new Set(current)
        next.delete(path)
        return next
      })
    }
  }

  function markExpanded(path: string) {
    setExpanded((current) => {
      if (current.has(path)) return current
      const next = new Set(current)
      next.add(path)
      return next
    })
  }

  async function expandTreeToPath(rawTarget: string, providedRoots?: DirectoryEntry[]) {
    expansionAbortRef.current?.abort()
    const controller = new AbortController()
    expansionAbortRef.current = controller
    const run = ++expansionRunRef.current
    const target = rawTarget.trim()
    if (!target) return

    let canonicalTarget = target
    const validatedTarget = await loadDirectory(target, controller.signal, false)
    if (controller.signal.aborted || run !== expansionRunRef.current) return
    if (validatedTarget?.path) canonicalTarget = validatedTarget.path

    const roots = providedRoots ?? rootsRef.current
    const root = roots
      .filter((candidate) => pathWithin(canonicalTarget, candidate.path))
      .sort((left, right) => normalizeForCompare(right.path).length - normalizeForCompare(left.path).length)[0]
      ?? roots
        .filter((candidate) => pathWithin(target, candidate.path))
        .sort((left, right) => normalizeForCompare(right.path).length - normalizeForCompare(left.path).length)[0]

    if (!root) {
      setSelected(target)
      return
    }

    let currentPath = root.path
    const segments = pathSegments(canonicalTarget, root.path)
    markExpanded(currentPath)

    for (const segment of segments) {
      if (controller.signal.aborted || run !== expansionRunRef.current) return
      const listing = await loadDirectory(currentPath, controller.signal)
      if (controller.signal.aborted || run !== expansionRunRef.current) return

      const candidatePath = joinPath(currentPath, segment)
      let child = (listing?.directories ?? listingsRef.current[currentPath] ?? []).find((entry) =>
        entry.name === segment || normalizeForCompare(entry.path) === normalizeForCompare(candidatePath)
      )

      if (!child) {
        const childListing = await loadDirectory(candidatePath, controller.signal, false)
        if (controller.signal.aborted || run !== expansionRunRef.current) return
        if (!childListing) {
          setSelected(currentPath)
          return
        }
        child = { name: segment, path: childListing.path }
        injectChild(currentPath, child)
      }

      currentPath = child.path
      markExpanded(currentPath)
    }

    if (controller.signal.aborted || run !== expansionRunRef.current) return
    setSelected(currentPath)
  }

  useEffect(() => {
    const controller = new AbortController()

    async function loadRoots() {
      setLoading((current) => new Set(current).add(''))
      try {
        const listing = await request<DirectoryListing>('/project-directories', { signal: controller.signal })
        if (controller.signal.aborted) return
        const roots = listing.directories ?? []
        rootsRef.current = roots
        listingsRef.current = { ...listingsRef.current, '': roots }
        setListings(listingsRef.current)
        markExpanded('')
        if (!valueRef.current && roots.length > 0) setSelected(roots[0].path)
        if (valueRef.current) void expandTreeToPath(valueRef.current, roots)
      } catch (error) {
        if (!controller.signal.aborted && !isAbortError(error)) {
          setErrors((current) => ({ ...current, '': errorMessage(error) }))
        }
      } finally {
        if (!controller.signal.aborted) {
          setLoading((current) => {
            const next = new Set(current)
            next.delete('')
            return next
          })
        }
      }
    }

    void loadRoots()
    return () => {
      controller.abort()
      expansionAbortRef.current?.abort()
    }
  }, [])

  useEffect(() => {
    setSelected(value)
    if (value.trim() && rootsRef.current.length > 0) {
      void expandTreeToPath(value)
    }
  }, [value])

  useEffect(() => {
    selectedRowRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [selected, expanded])

  async function toggle(path: string) {
    if (expanded.has(path)) {
      setExpanded((current) => {
        const next = new Set(current)
        next.delete(path)
        return next
      })
      return
    }

    let resolvedPath = path
    if (!Object.prototype.hasOwnProperty.call(listingsRef.current, path)) {
      const listing = await loadDirectory(path)
      if (!listing) return
      resolvedPath = listing.path
    }
    markExpanded(resolvedPath)
  }

  function selectPath(path: string) {
    setSelected(path)
    onSelect(path)
  }

  function chooseAndClose(path: string) {
    selectPath(path)
    onClose()
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
        ref={selected === path ? selectedRowRef : undefined}
        className={`directory-tree-row${selected === path ? ' is-selected' : ''}`}
        style={{ paddingLeft: `${10 + depth * 22}px` }}
        role="treeitem"
        aria-level={depth + 1}
        aria-selected={selected === path}
        aria-expanded={cycle ? undefined : isExpanded}
        title={path}
        onDoubleClick={() => chooseAndClose(path)}
      >
        <button
          type="button"
          className="directory-tree-toggle"
          aria-label={cycle ? `Cykl ${label}` : isExpanded ? `Zwiń ${label}` : `Rozwiń ${label}`}
          onClick={() => { if (!cycle) void toggle(path) }}
          disabled={isLoading || cycle || depth >= 32}
        >
          <Chevron expanded={isExpanded} loading={isLoading} cycle={cycle} />
        </button>
        <button
          type="button"
          className="directory-tree-name"
          onClick={() => selectPath(path)}
        >
          <FolderIcon open={isExpanded} />
          <span className="directory-tree-label">{label}</span>
        </button>
      </div>
      {cycle && <div className="directory-tree-empty" style={{ paddingLeft: `${44 + depth * 22}px` }}>Pominięto cykl dowiązania symbolicznego</div>}
      {errors[path] && <div className="directory-tree-error" style={{ paddingLeft: `${44 + depth * 22}px` }}>{errors[path]}</div>}
      {!cycle && isExpanded && children.map((child) => renderNode(child.path, child.name, depth + 1, nextAncestors))}
      {!cycle && isExpanded && truncated.has(path) &&
        <div className="directory-tree-empty" style={{ paddingLeft: `${44 + depth * 22}px` }}>Lista ograniczona do 500 katalogów</div>}
      {!cycle && isExpanded && !isLoading && !errors[path] && children.length === 0 &&
        <div className="directory-tree-empty" style={{ paddingLeft: `${44 + depth * 22}px` }}>Brak podkatalogów</div>}
    </div>
  }

  const roots = listings[''] ?? []

  return <div className="directory-picker">
    <div className="directory-picker-header">
      <div>
        <strong>Wybierz katalog</strong>
        <span className="muted">Drzewo automatycznie otwiera aktualną ścieżkę. Kliknięcie folderu od razu synchronizuje pole.</span>
      </div>
      <button type="button" className="secondary" onClick={onClose}>Zamknij</button>
    </div>

    <div className="directory-tree-shell">
      <div className="directory-tree-title">
        <FolderIcon open />
        <strong>Katalogi</strong>
      </div>
      <div className="directory-tree" role="tree" aria-label="Drzewo katalogów">
        {loading.has('') && <div className="directory-tree-empty directory-tree-root-message">Ładowanie katalogów…</div>}
        {errors[''] && <div className="directory-tree-error directory-tree-root-message">{errors['']}</div>}
        {!loading.has('') && !errors[''] && roots.map((root) => renderNode(root.path, root.name || root.path, 0, new Set()))}
        {!loading.has('') && !errors[''] && roots.length === 0 && <div className="directory-tree-empty directory-tree-root-message">Brak skonfigurowanych katalogów do przeglądania</div>}
      </div>
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
          chooseAndClose(selected)
        }}
      >
        Wybierz katalog
      </button>
    </div>
  </div>
}

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
  const message = error instanceof Error ? error.message : String(error)
  const normalized = message.toLowerCase()
  if (normalized.includes('does not exist')) return 'Katalog nie istnieje.'
  if (normalized.includes('permission') || normalized.includes('access denied')) return 'Brak dostępu do katalogu.'
  if (normalized.includes('outside configured browse roots')) return 'Katalog znajduje się poza dozwolonym zakresem.'
  return message || 'Nie udało się odczytać katalogu.'
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
  return normalizedTarget === normalizedRoot || normalizedTarget.startsWith(normalizedRoot + '/')
}

function pathSegments(target: string, root: string) {
  const normalizedTarget = normalizeForCompare(target)
  const normalizedRoot = normalizeForCompare(root)
  if (!pathWithin(normalizedTarget, normalizedRoot) || normalizedTarget === normalizedRoot) return []
  return normalizedTarget.slice(normalizedRoot.length + 1).split('/').filter(Boolean)
}

function joinPath(base: string, segment: string) {
  const separator = base.includes('\\') && !base.includes('/') ? '\\' : '/'
  return base.endsWith(separator) ? base + segment : base + separator + segment
}

function baseName(path: string) {
  const normalized = normalizeForCompare(path)
  const parts = normalized.split('/').filter(Boolean)
  return parts[parts.length - 1] || normalized || '/'
}

function Chevron({ expanded, loading, cycle }: { expanded: boolean; loading: boolean; cycle: boolean }) {
  if (loading) return <span className="directory-tree-spinner" aria-hidden="true" />
  if (cycle) return <span className="directory-tree-cycle" aria-hidden="true">↻</span>
  return <span className={'directory-tree-chevron' + (expanded ? ' is-expanded' : '')} aria-hidden="true" />
}

function FolderIcon({ open = false }: { open?: boolean }) {
  return <span className={'directory-tree-folder' + (open ? ' is-open' : '')} aria-hidden="true">
    <span className="directory-tree-folder-tab" />
  </span>
}

export function DirectoryPicker({ value, onSelect, onClose }: DirectoryPickerProps) {
  const [selected, setSelected] = useState(value.trim())
  const [currentPath, setCurrentPath] = useState('')
  const [roots, setRoots] = useState<DirectoryEntry[]>([])
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [listings, setListings] = useState<Record<string, DirectoryListing>>({})
  const [loading, setLoading] = useState<Set<string>>(new Set())
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [newFolderOpen, setNewFolderOpen] = useState(false)
  const [newFolderName, setNewFolderName] = useState('')
  const [createBusy, setCreateBusy] = useState(false)
  const [createError, setCreateError] = useState('')

  const listingsRef = useRef<Record<string, DirectoryListing>>({})
  const rootsRef = useRef<DirectoryEntry[]>([])
  const expansionAbortRef = useRef<AbortController | null>(null)
  const expansionRunRef = useRef(0)
  const selectedRowRef = useRef<HTMLDivElement | null>(null)

  function cacheListing(requestedPath: string, listing: DirectoryListing) {
    const normalizedListing: DirectoryListing = {
      ...listing,
      directories: [...(listing.directories ?? [])].sort((left, right) =>
        left.name.localeCompare(right.name, undefined, { sensitivity: 'base' })
      ),
    }
    const next = {
      ...listingsRef.current,
      [requestedPath]: normalizedListing,
      [normalizedListing.path]: normalizedListing,
    }
    listingsRef.current = next
    setListings(next)
  }

  function injectChild(parentPath: string, child: DirectoryEntry) {
    const current = listingsRef.current[parentPath]
    if (!current || current.directories.some((entry) => entry.path === child.path)) return
    const updated: DirectoryListing = {
      ...current,
      directories: [...current.directories, child].sort((left, right) =>
        left.name.localeCompare(right.name, undefined, { sensitivity: 'base' })
      ),
    }
    cacheListing(parentPath, updated)
  }

  async function loadDirectory(
    path: string,
    options: { signal?: AbortSignal; showError?: boolean; force?: boolean } = {},
  ): Promise<DirectoryListing | null> {
    const key = path
    if (!options.force && Object.prototype.hasOwnProperty.call(listingsRef.current, key)) {
      return listingsRef.current[key]
    }

    setLoading((current) => new Set(current).add(key))
    if (options.showError !== false) {
      setErrors((current) => {
        const next = { ...current }
        delete next[key]
        return next
      })
    }

    try {
      const suffix = path ? '?path=' + encodeURIComponent(path) : ''
      const listing = await request<DirectoryListing>('/project-directories' + suffix, { signal: options.signal })
      if (options.signal?.aborted) return null
      cacheListing(key, listing)
      return listing
    } catch (error) {
      if (!options.signal?.aborted && !isAbortError(error) && options.showError !== false) {
        setErrors((current) => ({ ...current, [key]: errorMessage(error) }))
      }
      return null
    } finally {
      setLoading((current) => {
        const next = new Set(current)
        next.delete(key)
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
    const targetListing = await loadDirectory(target, { signal: controller.signal, showError: false })
    if (controller.signal.aborted || run !== expansionRunRef.current) return
    if (targetListing?.path) canonicalTarget = targetListing.path

    const availableRoots = providedRoots ?? rootsRef.current
    const root = availableRoots
      .filter((candidate) => pathWithin(canonicalTarget, candidate.path))
      .sort((left, right) => normalizeForCompare(right.path).length - normalizeForCompare(left.path).length)[0]
      ?? availableRoots
        .filter((candidate) => pathWithin(target, candidate.path))
        .sort((left, right) => normalizeForCompare(right.path).length - normalizeForCompare(left.path).length)[0]

    if (!root) {
      const fallback = availableRoots[0]
      if (fallback) {
        const fallbackListing = await loadDirectory(fallback.path, { signal: controller.signal })
        if (fallbackListing) {
          setCurrentPath(fallbackListing.path)
          setSelected(fallbackListing.path)
          markExpanded(fallbackListing.path)
        }
      }
      return
    }

    let cursor = root.path
    markExpanded(cursor)
    for (const segment of pathSegments(canonicalTarget, root.path)) {
      if (controller.signal.aborted || run !== expansionRunRef.current) return
      const listing = await loadDirectory(cursor, { signal: controller.signal })
      if (!listing) return

      const expectedPath = joinPath(cursor, segment)
      let child = listing.directories.find((entry) =>
        entry.name === segment || normalizeForCompare(entry.path) === normalizeForCompare(expectedPath)
      )

      if (!child) {
        const childListing = await loadDirectory(expectedPath, { signal: controller.signal, showError: false })
        if (!childListing) {
          const fallbackListing = await loadDirectory(cursor, { signal: controller.signal })
          if (fallbackListing) {
            setCurrentPath(fallbackListing.path)
            setSelected(fallbackListing.path)
          }
          return
        }
        child = { name: segment, path: childListing.path }
        injectChild(cursor, child)
      }

      cursor = child.path
      markExpanded(cursor)
    }

    const finalListing = await loadDirectory(cursor, { signal: controller.signal })
    if (controller.signal.aborted || run !== expansionRunRef.current || !finalListing) return
    setCurrentPath(finalListing.path)
    setSelected(finalListing.path)
  }

  useEffect(() => {
    const controller = new AbortController()

    async function initialize() {
      const rootListing = await loadDirectory('', { signal: controller.signal })
      if (controller.signal.aborted || !rootListing) return
      const availableRoots = rootListing.directories ?? []
      rootsRef.current = availableRoots
      setRoots(availableRoots)

      if (value.trim()) {
        await expandTreeToPath(value, availableRoots)
        return
      }
      if (availableRoots[0]) {
        const listing = await loadDirectory(availableRoots[0].path, { signal: controller.signal })
        if (controller.signal.aborted || !listing) return
        setCurrentPath(listing.path)
        setSelected(listing.path)
        markExpanded(listing.path)
      }
    }

    void initialize()
    return () => {
      controller.abort()
      expansionAbortRef.current?.abort()
    }
  }, [])

  useEffect(() => {
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [onClose])

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
    const listing = await loadDirectory(path)
    if (!listing) return
    markExpanded(listing.path)
  }

  async function navigateTo(path: string) {
    const listing = await loadDirectory(path)
    if (!listing) return
    setCurrentPath(listing.path)
    setSelected(listing.path)
    markExpanded(listing.path)
    void expandTreeToPath(listing.path)
  }

  async function refreshCurrent() {
    if (!currentPath) return
    await loadDirectory(currentPath, { force: true })
  }

  async function createFolder() {
    const name = newFolderName.trim()
    if (!currentPath || !name || createBusy) return
    setCreateBusy(true)
    setCreateError('')
    try {
      const entry = await request<DirectoryEntry>('/project-directories', {
        method: 'POST',
        body: JSON.stringify({ parent: currentPath, name }),
      })
      await loadDirectory(currentPath, { force: true })
      injectChild(currentPath, entry)
      setSelected(entry.path)
      setNewFolderName('')
      setNewFolderOpen(false)
    } catch (error) {
      setCreateError(errorMessage(error))
    } finally {
      setCreateBusy(false)
    }
  }

  function breadcrumbItems() {
    if (!currentPath) return [] as DirectoryEntry[]
    const root = roots
      .filter((candidate) => pathWithin(currentPath, candidate.path))
      .sort((left, right) => normalizeForCompare(right.path).length - normalizeForCompare(left.path).length)[0]
    if (!root) return [{ name: baseName(currentPath), path: currentPath }]

    const items: DirectoryEntry[] = [{
      name: root.name && root.name !== root.path ? root.name : root.path,
      path: root.path,
    }]
    let cursor = root.path
    for (const segment of pathSegments(currentPath, root.path)) {
      cursor = joinPath(cursor, segment)
      items.push({ name: segment, path: cursor })
    }
    return items
  }

  function renderNode(path: string, label: string, depth: number, ancestors: Set<string>) {
    const cycle = ancestors.has(path)
    const isExpanded = expanded.has(path)
    const isLoading = loading.has(path)
    const listing = listings[path]
    const children = listing?.directories ?? []
    const nextAncestors = new Set(ancestors)
    nextAncestors.add(path)

    return <div key={String(depth) + ':' + path} className="directory-tree-node">
      <div
        ref={selected === path ? selectedRowRef : undefined}
        className={'directory-tree-row' + (selected === path ? ' is-selected' : '')}
        style={{ paddingLeft: String(8 + depth * 20) + 'px' }}
        role="treeitem"
        aria-level={depth + 1}
        aria-selected={selected === path}
        aria-expanded={cycle ? undefined : isExpanded}
        title={path}
      >
        <button
          type="button"
          className="directory-tree-toggle"
          aria-label={cycle ? 'Cykl ' + label : isExpanded ? 'Zwiń ' + label : 'Rozwiń ' + label}
          onClick={() => { if (!cycle) void toggle(path) }}
          disabled={isLoading || cycle || depth >= 32}
        >
          <Chevron expanded={isExpanded} loading={isLoading} cycle={cycle} />
        </button>
        <button
          type="button"
          className="directory-tree-name"
          onClick={() => void navigateTo(path)}
          onDoubleClick={() => void navigateTo(path)}
        >
          <FolderIcon open={isExpanded} />
          <span className="directory-tree-label">{label}</span>
        </button>
      </div>
      {cycle && <div className="directory-tree-empty" style={{ paddingLeft: String(42 + depth * 20) + 'px' }}>Pominięto cykl dowiązania symbolicznego</div>}
      {errors[path] && <div className="directory-tree-error" style={{ paddingLeft: String(42 + depth * 20) + 'px' }}>{errors[path]}</div>}
      {!cycle && isExpanded && children.map((child) => renderNode(child.path, child.name, depth + 1, nextAncestors))}
      {!cycle && isExpanded && listing?.truncated &&
        <div className="directory-tree-empty" style={{ paddingLeft: String(42 + depth * 20) + 'px' }}>Lista ograniczona do 500 katalogów</div>}
      {!cycle && isExpanded && !isLoading && !errors[path] && listing && children.length === 0 &&
        <div className="directory-tree-empty" style={{ paddingLeft: String(42 + depth * 20) + 'px' }}>Brak podkatalogów</div>}
    </div>
  }

  const currentListing = currentPath ? listings[currentPath] : undefined
  const currentDirectories = currentListing?.directories ?? []
  const currentLoading = currentPath ? loading.has(currentPath) : loading.has('')
  const currentError = currentPath ? errors[currentPath] : errors['']
  const crumbs = breadcrumbItems()

  return <div className="directory-picker-backdrop" role="presentation">
    <section className="directory-picker-modal" role="dialog" aria-modal="true" aria-labelledby="directory-picker-title">
      <header className="directory-picker-header">
        <div>
          <strong id="directory-picker-title">Wybierz katalog</strong>
          <span className="muted">System plików hosta DevBox / Linux / WSL</span>
        </div>
        <button type="button" className="directory-picker-close" aria-label="Zamknij eksplorator katalogów" onClick={onClose}>×</button>
      </header>

      <nav className="directory-breadcrumbs" aria-label="Ścieżka katalogu">
        {crumbs.length === 0 && <span className="muted">Ładowanie ścieżki…</span>}
        {crumbs.map((item, index) => <span key={item.path} className="directory-breadcrumb-item">
          {index > 0 && <span className="directory-breadcrumb-separator" aria-hidden="true">›</span>}
          <button type="button" title={item.path} onClick={() => void navigateTo(item.path)}>{item.name}</button>
        </span>)}
      </nav>

      <div className="directory-picker-body">
        <aside className="directory-tree-pane">
          <div className="directory-pane-heading"><strong>Drzewo katalogów</strong></div>
          <div className="directory-tree" role="tree" aria-label="Drzewo katalogów">
            {loading.has('') && <div className="directory-tree-empty directory-tree-root-message">Ładowanie katalogów…</div>}
            {errors[''] && <div className="directory-tree-error directory-tree-root-message">{errors['']}</div>}
            {!loading.has('') && !errors[''] && roots.map((root) => renderNode(root.path, root.name || root.path, 0, new Set()))}
            {!loading.has('') && !errors[''] && roots.length === 0 && <div className="directory-tree-empty directory-tree-root-message">Brak skonfigurowanych katalogów do przeglądania</div>}
          </div>
        </aside>

        <main className="directory-content-pane">
          <div className="directory-content-toolbar">
            <div className="directory-current-path" title={currentPath}><FolderIcon open /><code>{currentPath || '—'}</code></div>
            <div className="directory-content-actions">
              <button type="button" className="secondary" disabled={!currentPath || currentLoading} onClick={() => void refreshCurrent()}>Odśwież</button>
              <button type="button" className="secondary" disabled={!currentPath} onClick={() => {
                setNewFolderOpen((open) => !open)
                setCreateError('')
              }}>+ Nowy katalog</button>
            </div>
          </div>

          {newFolderOpen && <div className="directory-create-row">
            <label htmlFor="directory-new-folder-name">Nazwa katalogu</label>
            <input
              id="directory-new-folder-name"
              value={newFolderName}
              autoFocus
              maxLength={255}
              onChange={(event) => setNewFolderName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault()
                  void createFolder()
                }
              }}
              placeholder="nowa-aplikacja"
            />
            <button type="button" disabled={!newFolderName.trim() || createBusy} onClick={() => void createFolder()}>
              {createBusy ? 'Tworzenie…' : 'Utwórz'}
            </button>
            <button type="button" className="secondary" disabled={createBusy} onClick={() => {
              setNewFolderOpen(false)
              setNewFolderName('')
              setCreateError('')
            }}>Anuluj</button>
            {createError && <div className="directory-create-error" role="alert">{createError}</div>}
          </div>}

          <div className="directory-content-list" role="listbox" aria-label="Zawartość katalogu">
            <div className="directory-content-list-head"><span>Nazwa</span><span>Typ</span></div>
            {currentLoading && <div className="directory-content-status"><span className="directory-tree-spinner" aria-hidden="true" /> Ładowanie katalogu…</div>}
            {!currentLoading && currentError && <div className="directory-content-status is-error">
              <span>{currentError}</span>
              <button type="button" className="secondary" onClick={() => void refreshCurrent()}>Spróbuj ponownie</button>
            </div>}
            {!currentLoading && !currentError && currentDirectories.map((item) => <button
              type="button"
              key={item.path}
              className={'directory-content-row' + (selected === item.path ? ' is-selected' : '')}
              role="option"
              aria-selected={selected === item.path}
              title={item.path}
              onClick={() => setSelected(item.path)}
              onDoubleClick={() => void navigateTo(item.path)}
            >
              <span className="directory-content-name"><FolderIcon /><span>{item.name}</span></span>
              <span className="directory-content-type">Folder</span>
            </button>)}
            {!currentLoading && !currentError && currentListing && currentDirectories.length === 0 &&
              <div className="directory-content-status">Ten katalog nie zawiera podkatalogów.</div>}
            {!currentLoading && !currentError && currentListing?.truncated &&
              <div className="directory-content-status">Lista została ograniczona do 500 katalogów.</div>}
          </div>
        </main>
      </div>

      <footer className="directory-picker-selection">
        <div>
          <span className="muted">Wybrano</span>
          <code title={selected}>{selected || '—'}</code>
        </div>
        <div className="directory-picker-footer-actions">
          <button type="button" className="secondary" onClick={onClose}>Anuluj</button>
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
      </footer>
    </section>
  </div>
}

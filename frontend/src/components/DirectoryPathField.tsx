import { useEffect, useRef, useState, type FocusEvent, type KeyboardEvent } from 'react'
import { request } from '../api/client'
import { DirectoryPicker } from './DirectoryPicker'

interface DirectoryEntry {
  name: string
  path: string
}

interface DirectoryListing {
  path: string
  directories: DirectoryEntry[]
}

interface DirectorySuggestions {
  path: string
  items: DirectoryEntry[]
}

interface DirectoryPathFieldProps {
  id: string
  label: string
  value: string
  onChange: (path: string) => void
  disabled?: boolean
  required?: boolean
  helpText?: string
}

function isAbortError(error: unknown) {
  return typeof error === 'object' && error !== null && 'name' in error && (error as { name?: string }).name === 'AbortError'
}

function validationMessage(error: unknown) {
  const message = error instanceof Error ? error.message : String(error)
  const normalized = message.toLowerCase()
  if (normalized.includes('does not exist')) return 'Katalog nie istnieje.'
  if (normalized.includes('outside configured browse roots') || normalized.includes('access denied')) {
    return 'Katalog znajduje się poza dozwolonym zakresem.'
  }
  if (normalized.includes('must be absolute')) return 'Podaj bezwzględną ścieżkę do katalogu.'
  if (normalized.includes('not a directory')) return 'Wskazana ścieżka nie jest katalogiem.'
  if (normalized.includes('parent directory traversal')) return 'Ścieżka zawierająca „..” nie jest dozwolona.'
  return message || 'Nie udało się zweryfikować katalogu.'
}

export function DirectoryPathField({
  id,
  label,
  value,
  onChange,
  disabled = false,
  required = false,
  helpText,
}: DirectoryPathFieldProps) {
  const [browserOpen, setBrowserOpen] = useState(false)
  const [treePath, setTreePath] = useState(value)
  const [typing, setTyping] = useState(false)
  const [suggestions, setSuggestions] = useState<DirectoryEntry[]>([])
  const [suggestionsLoading, setSuggestionsLoading] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const [validationError, setValidationError] = useState('')
  const userEditingRef = useRef(false)
  const validationRunRef = useRef(0)
  const validationAbortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    if (!userEditingRef.current && value && value !== treePath) {
      setTreePath(value)
    }
  }, [value, treePath])

  useEffect(() => {
    if (disabled || !typing || !value.trim()) {
      setSuggestions([])
      setSuggestionsLoading(false)
      return
    }

    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      setSuggestionsLoading(true)
      request<DirectorySuggestions>(`/project-directories?suggest=${encodeURIComponent(value)}`, { signal: controller.signal })
        .then((result) => {
          if (controller.signal.aborted) return
          setSuggestions(result.items ?? [])
          setActiveIndex(0)
        })
        .catch((error: unknown) => {
          if (!controller.signal.aborted && !isAbortError(error)) {
            setSuggestions([])
          }
        })
        .finally(() => {
          if (!controller.signal.aborted) setSuggestionsLoading(false)
        })
    }, 250)

    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [disabled, typing, value])

  useEffect(() => () => validationAbortRef.current?.abort(), [])

  async function commitPath(rawPath: string, updateInput = true) {
    const candidate = rawPath.trim()
    if (!candidate) {
      setValidationError(required ? 'Podaj ścieżkę do katalogu.' : '')
      return false
    }

    if (updateInput) onChange(rawPath)
    userEditingRef.current = false
    setTyping(false)
    setSuggestions([])
    setSuggestionsLoading(false)
    setTreePath(candidate)

    validationAbortRef.current?.abort()
    const controller = new AbortController()
    validationAbortRef.current = controller
    const run = ++validationRunRef.current

    try {
      const listing = await request<DirectoryListing>(`/project-directories?path=${encodeURIComponent(candidate)}`, { signal: controller.signal })
      if (controller.signal.aborted || run !== validationRunRef.current) return false
      setValidationError('')
      setTreePath(listing.path)
      if (listing.path !== value || updateInput) onChange(listing.path)
      return true
    } catch (error) {
      if (controller.signal.aborted || run !== validationRunRef.current || isAbortError(error)) return false
      setValidationError(validationMessage(error))
      return false
    }
  }

  function handleInputChange(nextValue: string) {
    userEditingRef.current = true
    setTyping(true)
    setValidationError('')
    setActiveIndex(0)
    onChange(nextValue)
  }

  function chooseSuggestion(item: DirectoryEntry) {
    void commitPath(item.path)
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'ArrowDown' && suggestions.length > 0) {
      event.preventDefault()
      setActiveIndex((current) => (current + 1) % suggestions.length)
      return
    }
    if (event.key === 'ArrowUp' && suggestions.length > 0) {
      event.preventDefault()
      setActiveIndex((current) => (current - 1 + suggestions.length) % suggestions.length)
      return
    }
    if (event.key === 'Escape') {
      setTyping(false)
      setSuggestions([])
      return
    }
    if (event.key === 'Enter') {
      event.preventDefault()
      if (suggestions.length > 0 && suggestions[activeIndex]) {
        chooseSuggestion(suggestions[activeIndex])
      } else {
        void commitPath(value, false)
      }
    }
  }

  function handleBlur(event: FocusEvent<HTMLDivElement>) {
    const nextTarget = event.relatedTarget
    if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) return
    setTyping(false)
    setSuggestions([])
    if (userEditingRef.current && value.trim()) {
      void commitPath(value, false)
    }
  }

  function toggleBrowser() {
    if (browserOpen) {
      setBrowserOpen(false)
      return
    }
    setBrowserOpen(true)
    setTyping(false)
    setSuggestions([])
    if (value.trim()) {
      setTreePath(value.trim())
      void commitPath(value, false)
    }
  }

  function selectFromTree(path: string) {
    userEditingRef.current = false
    setTyping(false)
    setSuggestions([])
    setValidationError('')
    setTreePath(path)
    onChange(path)
    void commitPath(path, false)
  }

  const suggestionsVisible = typing && (suggestionsLoading || suggestions.length > 0)

  return <div className="span-2 path-picker-field path-picker-synchronized" onBlur={handleBlur}>
    <label htmlFor={id}>{label}</label>
    <div className={`path-picker-row${disabled ? ' is-disabled' : ''}`}>
      <div className="path-input-shell">
        <input
          id={id}
          disabled={disabled}
          value={value}
          onChange={(event) => handleInputChange(event.target.value)}
          onKeyDown={handleKeyDown}
          required={required}
          autoComplete="off"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={suggestionsVisible}
          aria-controls={`${id}-suggestions`}
          aria-invalid={Boolean(validationError)}
        />
        {suggestionsVisible && <div id={`${id}-suggestions`} className="path-suggestions" role="listbox">
          {suggestionsLoading && suggestions.length === 0 && <div className="path-suggestion-status">Wyszukiwanie katalogów…</div>}
          {suggestions.map((item, index) =>
            <button
              type="button"
              key={item.path}
              className={`path-suggestion${index === activeIndex ? ' is-active' : ''}`}
              role="option"
              aria-selected={index === activeIndex}
              onMouseDown={(event) => event.preventDefault()}
              onMouseEnter={() => setActiveIndex(index)}
              onClick={() => chooseSuggestion(item)}
            >
              <span className="path-suggestion-main"><span className="path-suggestion-folder" aria-hidden="true" />{item.name}</span>
              <code>{item.path}</code>
            </button>
          )}
        </div>}
      </div>
      {!disabled && <button type="button" className="secondary" onClick={toggleBrowser}>
        {browserOpen ? 'Ukryj drzewko' : 'Pokaż drzewko'}
      </button>}
    </div>
    {helpText && <span className="muted small">{helpText}</span>}
    {validationError && <span className="path-validation-error" role="alert">{validationError}</span>}
    {!disabled && browserOpen && <DirectoryPicker
      value={treePath}
      onSelect={selectFromTree}
      onClose={() => setBrowserOpen(false)}
    />}
  </div>
}

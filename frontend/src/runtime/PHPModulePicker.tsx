import { useMemo, useState } from 'react'
import type { RuntimeModuleOption } from '../api/types'

type ModuleFilter = 'all' | 'selected' | 'bundled' | 'pecl'

interface Props {
  catalog: RuntimeModuleOption[]
  selected: Set<string>
  disabled?: boolean
  loading?: boolean
  onToggle: (name: string, enabled: boolean) => void
  onSelectionChange: (names: string[]) => void
}

interface ModuleMeta {
  category: string
  delivery: 'bundled' | 'compiled' | 'pecl'
  deliveryLabel: string
}

const categoryOrder = [
  'Dane i bazy',
  'Web i tekst',
  'Pliki i grafika',
  'Cache i wydajność',
  'System i obliczenia',
  'Development',
  'Inne',
]

const categoryByModule: Record<string, string> = {
  pdo: 'Dane i bazy',
  pdo_mysql: 'Dane i bazy',
  mysqli: 'Dane i bazy',
  pgsql: 'Dane i bazy',
  sqlite3: 'Dane i bazy',
  mbstring: 'Web i tekst',
  intl: 'Web i tekst',
  curl: 'Web i tekst',
  xml: 'Web i tekst',
  soap: 'Web i tekst',
  gd: 'Pliki i grafika',
  imagick: 'Pliki i grafika',
  zip: 'Pliki i grafika',
  exif: 'Pliki i grafika',
  opcache: 'Cache i wydajność',
  redis: 'Cache i wydajność',
  memcached: 'Cache i wydajność',
  bcmath: 'System i obliczenia',
  gmp: 'System i obliczenia',
  ldap: 'System i obliczenia',
  sockets: 'System i obliczenia',
  pcntl: 'System i obliczenia',
  xdebug: 'Development',
}

const bundledModules = new Set(['pdo', 'sqlite3', 'mbstring', 'curl', 'xml'])
const peclModules = new Set(['imagick', 'redis', 'memcached', 'xdebug'])
const commonWebPreset = ['pdo_mysql', 'mbstring', 'intl', 'gd', 'curl', 'zip', 'opcache']

function moduleMeta(name: string): ModuleMeta {
  if (bundledModules.has(name)) {
    return {
      category: categoryByModule[name] ?? 'Inne',
      delivery: 'bundled',
      deliveryLabel: 'Wbudowany',
    }
  }
  if (peclModules.has(name)) {
    return {
      category: categoryByModule[name] ?? 'Inne',
      delivery: 'pecl',
      deliveryLabel: 'PECL',
    }
  }
  return {
    category: categoryByModule[name] ?? 'Inne',
    delivery: 'compiled',
    deliveryLabel: 'Rozszerzenie PHP',
  }
}

export function PHPModulePicker({
  catalog,
  selected,
  disabled = false,
  loading = false,
  onToggle,
  onSelectionChange,
}: Props) {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<ModuleFilter>('all')

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return catalog.filter((item) => {
      const meta = moduleMeta(item.name)
      if (needle && ![
        item.name,
        item.label,
        item.description,
        meta.category,
        meta.deliveryLabel,
      ].some((value) => value.toLowerCase().includes(needle))) {
        return false
      }
      if (filter === 'selected') return selected.has(item.name)
      if (filter === 'bundled') return meta.delivery === 'bundled'
      if (filter === 'pecl') return meta.delivery === 'pecl'
      return true
    })
  }, [catalog, filter, query, selected])

  const grouped = useMemo(() => categoryOrder
    .map((category) => ({
      category,
      items: visible.filter((item) => moduleMeta(item.name).category === category),
    }))
    .filter((group) => group.items.length > 0), [visible])

  const selectedItems = useMemo(
    () => catalog.filter((item) => selected.has(item.name)),
    [catalog, selected],
  )

  function addCommonWebPreset() {
    const available = new Set(catalog.map((item) => item.name))
    const next = new Set(selected)
    commonWebPreset.forEach((name) => {
      if (available.has(name)) next.add(name)
    })
    onSelectionChange([...next])
  }

  return <div className="php-module-picker">
    <div className="php-module-toolbar">
      <div className="php-module-search">
        <input
          aria-label="Szukaj modułów PHP"
          placeholder="Szukaj po nazwie, funkcji lub kategorii…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>

      <div className="php-module-filters" role="group" aria-label="Filtr modułów PHP">
        {([
          ['all', 'Wszystkie'],
          ['selected', `Wybrane (${selected.size})`],
          ['bundled', 'Wbudowane'],
          ['pecl', 'PECL'],
        ] as const).map(([value, label]) => <button
          key={value}
          type="button"
          className={filter === value ? 'active' : ''}
          aria-pressed={filter === value}
          onClick={() => setFilter(value)}
        >
          {label}
        </button>)}
      </div>

      {!disabled && <div className="php-module-quick-actions">
        <button type="button" className="secondary-button" onClick={addCommonWebPreset}>
          Dodaj typowe WWW
        </button>
        <button
          type="button"
          className="secondary-button"
          disabled={selected.size === 0}
          onClick={() => onSelectionChange([])}
        >
          Wyczyść wybór
        </button>
      </div>}
    </div>

    {loading
      ? <p className="muted">Wczytywanie katalogu modułów PHP…</p>
      : grouped.length > 0
        ? <div className="runtime-module-groups">
          {grouped.map((group) => <section className="runtime-module-group" key={group.category}>
            <div className="runtime-module-group-heading">
              <h4>{group.category}</h4>
              <span>{group.items.length}</span>
            </div>
            <div className="runtime-module-list">
              {group.items.map((item) => {
                const meta = moduleMeta(item.name)
                const checked = selected.has(item.name)
                return <label
                  className="runtime-module-row"
                  data-selected={checked}
                  key={item.name}
                >
                  <input
                    type="checkbox"
                    disabled={disabled}
                    checked={checked}
                    onChange={(event) => onToggle(item.name, event.target.checked)}
                  />
                  <span className="runtime-module-copy">
                    <span className="runtime-module-title">
                      <strong>{item.label}</strong>
                      <code>{item.name}</code>
                    </span>
                    <small>{item.description}</small>
                  </span>
                  <span className="runtime-module-delivery" data-kind={meta.delivery}>
                    {meta.deliveryLabel}
                  </span>
                </label>
              })}
            </div>
          </section>)}
        </div>
        : <div className="php-module-empty">
          <strong>Brak pasujących modułów</strong>
          <span>Zmień wyszukiwanie albo aktywny filtr.</span>
        </div>}

    <div className="php-module-summary">
      <div>
        <strong>{selected.size} {selected.size === 1 ? 'moduł wybrany' : 'modułów wybranych'}</strong>
        <span>Zmiana listy zostanie zastosowana przy przebudowie obrazu PHP.</span>
      </div>
      <div className="php-module-selected">
        {selectedItems.slice(0, 5).map((item) => <code key={item.name}>{item.name}</code>)}
        {selectedItems.length > 5 && <span>+{selectedItems.length - 5}</span>}
        {selectedItems.length === 0 && <span>Brak dodatkowych modułów</span>}
      </div>
    </div>
  </div>
}

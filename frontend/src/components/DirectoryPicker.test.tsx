import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { DirectoryPicker } from './DirectoryPicker'

describe('directory picker', () => {
  it('renders the directory explorer modal shell', () => {
    const html = renderToStaticMarkup(
      <DirectoryPicker value="/var/lib/devbox/projects" onSelect={() => undefined} onClose={() => undefined} />
    )

    expect(html).toContain('directory-picker-backdrop')
    expect(html).toContain('role="dialog"')
    expect(html).toContain('Drzewo katalogów')
    expect(html).toContain('Zawartość katalogu')
    expect(html).toContain('+ Nowy katalog')
    expect(html).toContain('Odśwież')
    expect(html).toContain('Wybierz katalog')
    expect(html).toContain('/var/lib/devbox/projects')
  })
})

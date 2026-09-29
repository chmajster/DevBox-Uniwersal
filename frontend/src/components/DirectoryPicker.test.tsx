import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { DirectoryPicker } from './DirectoryPicker'

describe('directory picker', () => {
  it('renders an explorer-style directory tree shell', () => {
    const html = renderToStaticMarkup(
      <DirectoryPicker value="/var/lib/devbox/projects" onSelect={() => undefined} onClose={() => undefined} />
    )

    expect(html).toContain('directory-tree-shell')
    expect(html).toContain('directory-tree-title')
    expect(html).toContain('directory-tree-folder')
    expect(html).toContain('Katalogi')
    expect(html).toContain('Drzewo katalogów')
    expect(html).toContain('Wybrana ścieżka')
    expect(html).toContain('/var/lib/devbox/projects')
  })
})

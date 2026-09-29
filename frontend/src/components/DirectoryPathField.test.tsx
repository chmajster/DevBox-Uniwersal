import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { DirectoryPathField } from './DirectoryPathField'

describe('directory path field', () => {
  it('renders one synchronized path input and tree toggle', () => {
    const html = renderToStaticMarkup(
      <DirectoryPathField
        id="project-path"
        label="Ścieżka do aplikacji"
        value="/mnt/c/Users/Chris/Documents/GitHub/OpenServiceNOW"
        onChange={() => undefined}
        required
        helpText="Test"
      />
    )

    expect(html).toContain('path-picker-synchronized')
    expect(html).toContain('role="combobox"')
    expect(html).toContain('Pokaż drzewko')
    expect(html).toContain('/mnt/c/Users/Chris/Documents/GitHub/OpenServiceNOW')
    expect(html).toContain('aria-autocomplete="list"')
  })
})

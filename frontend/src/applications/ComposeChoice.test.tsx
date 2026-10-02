import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ComposeChoice } from './ComposeChoice'

describe('ComposeChoice', () => {
  it('asks explicitly without selecting an answer', () => {
    const markup = renderToStaticMarkup(<ComposeChoice value="" onChange={() => {}} />)
    expect(markup).toContain('Czy chcesz go użyć?')
    expect(markup).not.toContain('checked=""')
    expect(markup).not.toContain('<select')
  })
  it('confirms Compose without offering a conflicting alternative', () => {
    const markup = renderToStaticMarkup(<ComposeChoice value="compose" onChange={() => {}} />)
    expect(markup.match(/checked=""/g)).toHaveLength(1)
    expect(markup).not.toContain('<select')
  })
  it.each(['managed', 'dockerfile'])('offers an explicit alternative after declining: %s', (driver) => {
    const markup = renderToStaticMarkup(<ComposeChoice value={driver} onChange={() => {}} />)
    expect(markup.match(/checked=""/g)).toHaveLength(1)
    expect(markup).toContain('<select')
    expect(markup).toContain('value="' + driver + '" selected=""')
  })
})

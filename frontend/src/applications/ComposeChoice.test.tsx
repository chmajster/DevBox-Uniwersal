import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ComposeChoice } from './ComposeChoice'

describe('ComposeChoice', () => {
  it('offers all three source deployment modes', () => {
    const markup = renderToStaticMarkup(<ComposeChoice value="" onChange={() => {}} />)
    expect(markup).toContain('Existing Docker Compose')
    expect(markup).toContain('DevBox Managed Runtime')
    expect(markup).not.toContain('checked=""')
    expect(markup).toContain('Dockerfile aplikacji')
    expect(markup.match(/type="radio"/g)).toHaveLength(3)
  })
  it('selects Compose without hiding Auto Container', () => {
    const markup = renderToStaticMarkup(<ComposeChoice value="compose" onChange={() => {}} />)
    expect(markup.match(/checked=""/g)).toHaveLength(1)
    expect(markup.match(/type="radio"/g)).toHaveLength(3)
  })
  it('selects Auto Container', () => {
    const markup = renderToStaticMarkup(<ComposeChoice value="auto" onChange={() => {}} />)
    expect(markup.match(/checked=""/g)).toHaveLength(1)
    expect(markup).toContain('DevBox Managed Runtime')
  })
})

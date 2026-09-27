import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { StatusBadge } from './StatusBadge'

describe('StatusBadge', () => {
  it.each(['RUNNING', 'STOPPED', 'FAILED', 'BUILDING', 'DEPLOYING', 'UNHEALTHY'])('renders text and icon for %s', (status) => {
    const markup = renderToStaticMarkup(<StatusBadge status={status} />)
    expect(markup).toContain(status)
    expect(markup).toContain('aria-hidden="true"')
  })
})

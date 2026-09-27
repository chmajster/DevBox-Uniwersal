import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { ErrorState } from './ErrorState'

describe('ErrorState', () => {
  it('preserves the concrete backend message', () => {
    const markup = renderToStaticMarkup(<ErrorState message="nginx -t failed on /etc/nginx/sites-enabled/app.conf" />)
    expect(markup).toContain('nginx -t failed on /etc/nginx/sites-enabled/app.conf')
    expect(markup).not.toContain('Something went wrong')
  })
})

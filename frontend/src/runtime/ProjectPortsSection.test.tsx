import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { PortSettingsFields } from './ProjectPortsSection'
import { defaultPortSettings } from './portSettings'

describe('port configuration fields', () => {
  it('renders distinct internal/external fields and high defaults', () => {
    const html = renderToStaticMarkup(<PortSettingsFields settings={defaultPortSettings} disabled={false} onChange={() => undefined} />)
    expect(html).toContain('Port wewnętrzny HTTP (Docker)')
    expect(html).toContain('Port zewnętrzny HTTP (host)')
    expect(html).toContain('value="8080"')
    expect(html).toContain('value="8443"')
    expect(html).toContain('max="65535"')
    expect(html).toContain('nie tworzy certyfikatu')
  })
  it('disables mutation controls for a read-only user', () => {
    const html = renderToStaticMarkup(<PortSettingsFields settings={defaultPortSettings} disabled onChange={() => undefined} />)
    expect(html).toContain('<fieldset disabled=""')
  })
})

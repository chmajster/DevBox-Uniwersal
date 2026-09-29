import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { PortSettingsFields } from './ProjectPortsSection'
import { defaultPortSettings } from './portSettings'

describe('port configuration fields', () => {
  it('renders a compact host-to-container mapping table', () => {
    const html = renderToStaticMarkup(<PortSettingsFields settings={defaultPortSettings} disabled={false} onChange={() => undefined} />)
    expect(html).toContain('Port hosta')
    expect(html).toContain('Port kontenera')
    expect(html).toContain('Typ')
    expect(html).toContain('aria-label="Port hosta HTTP"')
    expect(html).toContain('placeholder="Auto"')
    expect(html).toContain('value="8080"')
    expect(html).toContain('TCP')
    expect(html).toContain('+ Dodaj HTTPS')
  })

  it('renders the optional HTTPS mapping as another table row', () => {
    const html = renderToStaticMarkup(<PortSettingsFields settings={{ ...defaultPortSettings, https_enabled: true }} disabled={false} onChange={() => undefined} />)
    expect(html).toContain('aria-label="Port hosta HTTPS"')
    expect(html).toContain('value="8443"')
    expect(html).toContain('value="443"')
    expect(html).toContain('Usuń HTTPS')
    expect(html).toContain('nie tworzy certyfikatu')
  })

  it('disables mutation controls for a read-only user', () => {
    const html = renderToStaticMarkup(<PortSettingsFields settings={defaultPortSettings} disabled onChange={() => undefined} />)
    expect(html).toContain('<fieldset disabled=""')
  })
})

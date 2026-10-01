import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { endpointURL, readConfiguration, type Endpoint } from './model'
import { ApplicationStatus, ConfigurationFields } from './components'

const endpoint: Endpoint = { id: 'e', name: 'web', workload_id: 'w', protocol: 'http', container_port: 80, host_port: 8080, primary: true, public: true, status: 'running' }
describe('application control plane UI contract', () => {
  it('uses only published ports and supports IPv6', () => {
    expect(endpointURL(endpoint, 'devbox.example')).toBe('http://devbox.example:8080/')
    expect(endpointURL(endpoint, '2001:db8::1')).toBe('http://[2001:db8::1]:8080/')
    expect(endpointURL({ ...endpoint, host_port: undefined }, 'localhost')).toBeNull()
    expect(endpointURL({ ...endpoint, protocol: 'javascript' }, 'localhost')).toBeNull()
    expect(endpointURL({ ...endpoint, protocol: 'tcp' }, 'localhost')).toBeNull()
    expect(endpointURL({ ...endpoint, host_port: 65536 }, 'localhost')).toBeNull()
  })
  it('does not claim stopped for unknown state or success for failed deployments', () => {
    const unknown = renderToStaticMarkup(createElement(ApplicationStatus, { value: 'unknown' }))
    expect(unknown).toContain('Brak odczytu'); expect(unknown).not.toContain('Zatrzymana')
    expect(renderToStaticMarkup(createElement(ApplicationStatus, { value: 'waiting_for_configuration' }))).toContain('Wymaga konfiguracji')
    expect(renderToStaticMarkup(createElement(ApplicationStatus, { value: 'failed' }))).toContain('Błąd')
  })
  it('builds typed configuration without accidentally turning blank ports into invalid values', () => {
    const form = new FormData(); form.set('container_port', '8080'); form.set('host_port', '0')
    form.set('modules', 'gd, zip pdo_mysql'); form.set('environment', 'APP_ENV=test\nPUBLIC_URL=https://example.test/?a=b')
    expect(readConfiguration(form)).toEqual({ container_port: 8080, host_port: 0, modules: ['gd', 'zip', 'pdo_mysql'], environment: { APP_ENV: 'test', PUBLIC_URL: 'https://example.test/?a=b' } })
    form.set('container_port', '1.5'); expect(() => readConfiguration(form)).toThrow()
    form.set('container_port', ''); form.set('environment', 'invalid line'); expect(() => readConfiguration(form)).toThrow()
    expect(readConfiguration(new FormData())).toEqual({})
  })
  it('renders saved values without exposing a secret editor as a plain text configuration field', () => {
    const html = renderToStaticMarkup(createElement(ConfigurationFields, { value: { container_port: 8080, modules: ['gd', 'zip'], environment: { APP_ENV: 'development' } } }))
    expect(html).toContain('8080'); expect(html).toContain('gd, zip'); expect(html).toContain('APP_ENV=development')
    expect(html).toContain('zakładce Sekrety'); expect(html).not.toContain('name="password"')
  })
})

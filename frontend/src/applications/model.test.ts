import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { endpointURL, readConfiguration, validateManagedConfiguration, type Endpoint } from './model'
import { ApplicationStatus, ConfigurationFields } from './components'

const endpoint: Endpoint = { id: 'e', name: 'web', workload_id: 'w', protocol: 'http', container_port: 80, host_port: 8080, primary: true, public: true, status: 'running' }
describe('application control plane UI contract', () => {
  it('uses only published ports and supports IPv6', () => {
    expect(endpointURL(endpoint, 'devbox.example')).toBe('http://devbox.example:8080/')
    expect(endpointURL(endpoint, '2001:db8::1')).toBe('http://[2001:db8::1]:8080/')
    expect(endpointURL({ ...endpoint, host_port: undefined }, 'localhost')).toBeNull()
    expect(endpointURL({ ...endpoint, protocol: 'javascript' }, 'localhost')).toBeNull()
    expect(endpointURL({ ...endpoint, protocol: 'tcp' }, 'localhost')).toBeNull()
    expect(endpointURL({ ...endpoint, status: 'missing' }, 'localhost')).toBeNull()
    expect(endpointURL({ ...endpoint, status: 'stopped' }, 'localhost')).toBeNull()
    expect(endpointURL({ ...endpoint, domain: 'app.local', tls_mode: 'existing', route_active: true }, 'localhost')).toBe('https://app.local/')
    expect(endpointURL({ ...endpoint, host_port: 65536 }, 'localhost')).toBeNull()
  })
  it('does not claim stopped for unknown state or success for failed deployments', () => {
    const unknown = renderToStaticMarkup(createElement(ApplicationStatus, { value: 'unknown' }))
    expect(unknown).toContain('UNKNOWN'); expect(unknown).not.toContain('Zatrzymana')
    expect(renderToStaticMarkup(createElement(ApplicationStatus, { value: 'waiting_for_configuration' }))).toContain('Wymaga konfiguracji')
    expect(renderToStaticMarkup(createElement(ApplicationStatus, { value: 'failed' }))).toContain('FAILED')
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
    const html = renderToStaticMarkup(createElement(ConfigurationFields, { value: { runtime: 'php', container_port: 8080, modules: ['gd', 'zip'], environment: { APP_ENV: 'development' } }, deploymentMode: 'auto' }))
    expect(html).toContain('8080'); expect(html).toContain('gd, zip'); expect(html).toContain('APP_ENV'); expect(html).toContain('development')
    expect(html).toContain('SecretStore'); expect(html).not.toContain('name="password"')
  })
})

describe('explicit managed container settings', () => {
  it('requires language and version while allowing Compose', () => {
    expect(() => validateManagedConfiguration({ deployment_mode: 'auto' })).toThrow('Wybierz język')
    expect(() => validateManagedConfiguration({ deployment_mode: 'auto', runtime: 'php' })).toThrow('Wybierz wersję')
    expect(() => validateManagedConfiguration({ deployment_mode: 'auto', runtime: 'php', runtime_version: '8.3.12' })).not.toThrow()
    expect(() => validateManagedConfiguration({ deployment_mode: 'auto', runtime: 'php', runtime_version: '8.3; echo bad' })).toThrow('poprawną wersję')
    expect(() => validateManagedConfiguration({ deployment_mode: 'compose' })).not.toThrow()
  })
  it('serializes explicit software, version and optional dependencies', () => {
    const form = new FormData()
    form.set('deployment_mode', 'auto'); form.set('runtime', 'node'); form.set('runtime_version', '22')
    form.set('modules', 'git, build-essential')
    expect(readConfiguration(form)).toEqual({ deployment_mode: 'auto', runtime: 'node', runtime_version: '22', modules: ['git', 'build-essential'] })
  })
  it('shows saved exact PHP versions and omits managed controls for Compose', () => {
    const value = { runtime: 'php', runtime_version: '8.3.12', modules: ['pdo_mysql'] }
    const html = renderToStaticMarkup(createElement(ConfigurationFields, { value, deploymentMode: 'auto' }))
    expect(html).toContain('value="8.3.12"'); expect(html).toContain('Wybór wersji')
    expect(html).not.toContain('Puste pole wybiera wersję domyślną')
    const compose = renderToStaticMarkup(createElement(ConfigurationFields, { value, deploymentMode: 'compose' }))
    expect(compose).not.toContain('name="runtime"'); expect(compose).not.toContain('name="runtime_version"'); expect(compose).not.toContain('name="modules"')
  })
  it('lets the user choose a PHP image version and turns a saved Composer constraint into a selectable version', () => {
    const html = renderToStaticMarkup(createElement(ConfigurationFields, { value: { runtime: 'php', runtime_version: '^8.2' }, deploymentMode: 'auto' }))
    expect(html).not.toContain('<option value="8.5">8.5</option>')
    expect(html).toContain('value="8.2"')
    expect(html).toContain('Ten wybór zastępuje zakres z composer.json')
    expect(html).not.toContain('value="^8.2"')
  })
})

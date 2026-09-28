import { describe, expect, it } from 'vitest'
import { defaultPortSettings, publishedApplicationURL, samePortSettings, validatePortSettings } from './portSettings'

describe('project port settings', () => {
  it('uses stable high host defaults, without pretending to enable TLS', () => {
    expect(defaultPortSettings).toMatchObject({ container_port: 0, host_port: 8080, https_enabled: false, https_container_port: 443, https_host_port: 8443 })
    expect(validatePortSettings(defaultPortSettings)).toBeNull()
  })
  it.each([0, -1, 65536, 8080.5, Number.NaN])('rejects invalid published port %s', (host_port) => {
    expect(validatePortSettings({ ...defaultPortSettings, host_port })).not.toBeNull()
  })
  it('allows automatic container detection but rejects invalid or ambiguous listeners', () => {
    expect(validatePortSettings({ ...defaultPortSettings, container_port: 80 })).toBeNull()
    expect(validatePortSettings({ ...defaultPortSettings, container_port: -1 })).not.toBeNull()
    expect(validatePortSettings({ ...defaultPortSettings, container_port: 443, https_enabled: true })).not.toBeNull()
    expect(validatePortSettings({ ...defaultPortSettings, compose_service: 'web\nports:' })).not.toBeNull()
  })
  it('distinguishes saved settings from the last applied settings', () => {
    expect(samePortSettings(defaultPortSettings, { ...defaultPortSettings })).toBe(true)
    expect(samePortSettings(defaultPortSettings, { ...defaultPortSettings, host_port: 9080 })).toBe(false)
  })
  it('builds correct links for HTTP, HTTPS and IPv6 without carrying tokens or paths', () => {
    expect(publishedApplicationURL('https://user:password@example.test:8787/projects?token=secret#details', 8081, false)).toBe('http://example.test:8081/')
    expect(publishedApplicationURL('http://[::1]:8787/projects', 8444, true)).toBe('https://[::1]:8444/')
  })
})

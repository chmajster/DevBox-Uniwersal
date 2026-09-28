import { describe, expect, it } from 'vitest'
import { navigationForPath, navigationForRole, readPreference, savePreference } from './navigation'

describe('workspace navigation', () => {
  it('keeps privileged destinations out of viewer navigation and search', () => {
    const paths = navigationForRole('viewer').map((item) => item.to)
    expect(paths).not.toContain('/backups')
    expect(paths).not.toContain('/audit')
    expect(paths).toContain('/apps')
    expect(paths).toContain('/plugins')
    expect(new Set(paths).size).toBe(paths.length)
  })
  it('preserves operator and admin visibility', () => {
    expect(navigationForRole('operator').some((item) => item.to === '/audit')).toBe(true)
    expect(navigationForRole('operator').some((item) => item.to === '/backups')).toBe(false)
    expect(navigationForRole('admin').some((item) => item.to === '/backups')).toBe(true)
    expect(navigationForRole().some((item) => item.roles)).toBe(false)
  })
  it('matches canonical, nested and legacy application routes without matching prefixes', () => {
    expect(navigationForPath('/')?.label).toBe('Przegląd')
    expect(navigationForPath('/apps/new')?.to).toBe('/apps')
    expect(navigationForPath('/projects/id/runtime')?.to).toBe('/apps')
    expect(navigationForPath('/apps-other')).toBeUndefined()
    expect(navigationForPath('/plugins')?.label).toBe('Pluginy')
    expect(navigationForPath('/backups', 'viewer')).toBeUndefined()
  })
  it('does not require browser storage during server rendering', () => {
    expect(readPreference('devbox-test', 'fallback')).toBe('fallback')
    expect(() => savePreference('devbox-test', 'value')).not.toThrow()
  })
})

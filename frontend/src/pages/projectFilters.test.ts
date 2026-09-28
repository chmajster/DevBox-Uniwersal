import { describe, expect, it } from 'vitest'
import type { Project } from '../api/types'
import { filterProjects } from './projectFilters'

const projects = [
  { id: '1', name: 'Portal', runtime: 'PHP', status: 'active', branch: 'main', domain: 'portal.local' },
  { id: '2', name: 'API', runtime: 'Go', status: 'failed', description: 'Integracja' },
  { id: '3', name: 'Worker', runtime: 'Python', status: 'mystery' }
] as Project[]

describe('application filters', () => {
  it('searches case-insensitively across name, runtime, branch and domain', () => {
    for (const query of [' PORTAL ', 'php', 'MAIN', 'portal.local']) expect(filterProjects(projects, query, '')[0]?.id).toBe('1')
    expect(filterProjects(projects, 'integracja', '')[0]?.id).toBe('2')
  })
  it('combines search and normalized status without changing source order or data', () => {
    expect(filterProjects(projects, '', 'RUNNING').map((item) => item.id)).toEqual(['1'])
    expect(filterProjects(projects, 'Portal', 'FAILED')).toEqual([])
    expect(filterProjects(projects, '', 'UNHEALTHY').map((item) => item.id)).toEqual(['3'])
    expect(projects[0].status).toBe('active')
  })
  it('supports empty collections, empty queries and missing optional fields', () => {
    expect(filterProjects([], '', '')).toEqual([])
    expect(filterProjects(projects, '   ', '')).toHaveLength(3)
    expect(filterProjects(projects, 'does-not-exist', '')).toEqual([])
  })
})

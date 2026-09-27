import { matchPath } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import { PROJECT_TABS, ROUTES } from './routes'

describe('routing contract', () => {
  it('matches project detail tab routes', () => {
    const match = matchPath({ path: ROUTES.project, end: true }, '/projects/project-1/logs')
    expect(match?.params.projectId).toBe('project-1')
    expect(match?.params.tab).toBe('logs')
  })

  it('uses /apps as the canonical applications route', () => {
    expect(ROUTES.applications).toBe('/apps')
  })

  it('contains every required project details tab', () => {
    expect(PROJECT_TABS).toEqual([
      'overview',
      'configuration',
      'runtime',
      'git',
      'deployments',
      'logs',
      'environment',
      'database',
      'networking',
      'backups'
    ])
  })
})

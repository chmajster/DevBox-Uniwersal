import { describe, expect, it } from 'vitest'
import type { Project } from '../api/types'
import { projectApplicationURL } from './ProjectsPage'

describe('projectApplicationURL', () => {
  it('uses the explicit HTTP application URL when available', () => {
    const project = { domain: 'plan.localhost', open_url: 'https://plan.localhost' } as Project
    expect(projectApplicationURL(project)).toBe('https://plan.localhost')
  })

  it('builds an HTTP URL from a project domain', () => {
    const project = { domain: 'plan.localhost' } as Project
    expect(projectApplicationURL(project)).toBe('http://plan.localhost')
  })

  it('keeps a domain that already contains an HTTP scheme', () => {
    const project = { domain: 'http://plan.localhost' } as Project
    expect(projectApplicationURL(project)).toBe('http://plan.localhost')
  })

  it('returns an empty URL when the project has no domain', () => {
    const project = {} as Project
    expect(projectApplicationURL(project)).toBe('')
  })
})

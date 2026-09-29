import { describe, expect, it } from 'vitest'
import type { DockerContainer, Project } from '../api/types'
import { groupDockerContainers } from './dockerContainerGroups'

function container(name: string, extra: Partial<DockerContainer> = {}): DockerContainer {
  return {
    id: name + '-id',
    name,
    image: 'example:latest',
    state: 'running',
    status: 'Up',
    ...extra,
  }
}

function project(id: string, name: string, slug: string): Project {
  return {
    id,
    name,
    slug,
    description: '',
    status: 'running',
    source_type: 'local',
    local_path: '/srv/' + slug,
    runtime: '',
    runtime_version: '',
    container_policy: 'auto',
    working_directory: '',
    build_command: '',
    start_command: '',
    healthcheck: '',
    auto_start: false,
    created_at: '',
    updated_at: '',
  }
}

describe('groupDockerContainers', () => {
  it('groups Compose services into one application card', () => {
    const groups = groupDockerContainers([
      container('plan-web-1', { compose_project: 'plan' }),
      container('plan-db-1', { compose_project: 'plan' }),
    ], [project('project-plan', 'Plan', 'plan')])

    expect(groups).toHaveLength(1)
    expect(groups[0].key).toBe('project:project-plan')
    expect(groups[0].label).toBe('Plan')
    expect(groups[0].containers.map((item) => item.name)).toEqual(['plan-db-1', 'plan-web-1'])
  })

  it('maps managed containers by DevBox project ID', () => {
    const groups = groupDockerContainers([
      container('devbox-app-a1b2', { project_id: 'project-1' }),
    ], [project('project-1', 'OpenServiceNOW', 'openservicenow')])

    expect(groups[0].label).toBe('OpenServiceNOW')
    expect(groups[0].kind).toBe('project')
  })

  it('keeps DevBox infrastructure together and standalone containers separate', () => {
    const groups = groupDockerContainers([
      container('devbox-mysql'),
      container('devbox-phpmyadmin'),
      container('redis-test', { state: 'exited', status: 'Exited' }),
    ], [])

    expect(groups.map((group) => group.label)).toEqual(['DevBox / infrastruktura', 'Pozostałe kontenery'])
    expect(groups[0].running).toBe(2)
    expect(groups[1].stopped).toBe(1)
  })

  it('keeps different Compose projects in different cards', () => {
    const groups = groupDockerContainers([
      container('plan-web-1', { compose_project: 'plan' }),
      container('plan1-web-1', { compose_project: 'plan1', state: 'exited' }),
    ], [])

    expect(groups.map((group) => group.label)).toEqual(['plan', 'plan1'])
    expect(groups[1].stopped).toBe(1)
  })
})

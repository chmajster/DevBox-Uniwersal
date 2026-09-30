import { describe, expect, it } from 'vitest'
import type { DockerContainer, Project } from '../api/types'
import { resolveApplicationRuntimeStatus } from './applicationRuntimeStatus'

function container(name: string, extra: Partial<DockerContainer> = {}): DockerContainer {
  return {
    id: name + '-id',
    name,
    image: 'example:latest',
    state: 'running',
    status: 'Up 10 seconds',
    ...extra,
  }
}

function project(extra: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    name: 'Plan',
    slug: 'plan',
    description: '',
    status: 'ready',
    source_type: 'local',
    local_path: '/srv/plan',
    runtime: 'php',
    runtime_version: '',
    container_policy: 'auto',
    working_directory: '',
    build_command: '',
    start_command: '',
    healthcheck: '',
    auto_start: false,
    created_at: '',
    updated_at: '',
    ...extra,
  }
}

describe('resolveApplicationRuntimeStatus', () => {
  it('shows RUNNING from the live managed container even when persisted project status is error', () => {
    expect(resolveApplicationRuntimeStatus(
      project({ status: 'error' }),
      [container('devbox-app-project-1', { project_id: 'project-1' })],
      true,
    )).toBe('RUNNING')
  })

  it('shows RUNNING for a live Compose deployment matched to the application', () => {
    expect(resolveApplicationRuntimeStatus(
      project({ status: 'error' }),
      [
        container('plan-web-1', { compose_project: 'plan' }),
        container('plan-db-1', { compose_project: 'plan' }),
      ],
      true,
    )).toBe('RUNNING')
  })

  it('marks a partially stopped Compose application as UNHEALTHY', () => {
    expect(resolveApplicationRuntimeStatus(
      project({ status: 'running' }),
      [
        container('plan-web-1', { compose_project: 'plan' }),
        container('plan-db-1', { compose_project: 'plan', state: 'exited', status: 'Exited (0) 1 minute ago' }),
      ],
      true,
    )).toBe('UNHEALTHY')
  })

  it('marks Docker healthcheck failure as UNHEALTHY', () => {
    expect(resolveApplicationRuntimeStatus(
      project({ status: 'running' }),
      [container('plan-web-1', { compose_project: 'plan', status: 'Up 20 seconds (unhealthy)' })],
      true,
    )).toBe('UNHEALTHY')
  })

  it('marks non-zero container exit as FAILED', () => {
    expect(resolveApplicationRuntimeStatus(
      project({ status: 'running' }),
      [container('plan-web-1', { compose_project: 'plan', state: 'exited', status: 'Exited (1) 1 minute ago' })],
      true,
    )).toBe('FAILED')
  })

  it('marks a previously running application with no live containers as STOPPED', () => {
    expect(resolveApplicationRuntimeStatus(
      project({ status: 'running' }),
      [],
      true,
    )).toBe('STOPPED')
  })

  it('falls back to persisted project status when Docker cannot be queried', () => {
    expect(resolveApplicationRuntimeStatus(
      project({ status: 'error' }),
      [],
      false,
    )).toBe('error')
  })
})

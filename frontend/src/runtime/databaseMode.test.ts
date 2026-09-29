import { describe, expect, it } from 'vitest'
import { databaseModeChoice, databaseModeFields, dockerHostDatabaseHost, isDockerHostDatabaseHost } from './databaseMode'

describe('project database mode field contract', () => {
  it('renders no connection fields for none mode', () => {
    expect(databaseModeFields('none')).toEqual([])
  })

  it('renders managed database identity and application endpoint fields', () => {
    expect(databaseModeFields('managed')).toEqual([
      'application_service', 'engine', 'database', 'username', 'application_host', 'application_port', 'status', 'created_at',
    ])
  })

  it('renders Compose application and database service configuration', () => {
    expect(databaseModeFields('compose')).toEqual([
      'application_service', 'compose_service', 'port', 'database', 'username', 'password', 'application_host', 'application_port',
    ])
  })

  it('renders external endpoint and credential fields', () => {
    expect(databaseModeFields('external')).toEqual([
      'application_service', 'host', 'port', 'database', 'username', 'password', 'application_host', 'application_port',
    ])
  })

  it('recognizes Docker-host MySQL as a dedicated access-only UI choice', () => {
    expect(dockerHostDatabaseHost).toBe('host.docker.internal')
    for (const host of ['host.docker.internal', 'HOST.DOCKER.INTERNAL', 'localhost', '127.0.0.1', '::1', '[::1]']) {
      expect(isDockerHostDatabaseHost(host)).toBe(true)
    }
    expect(databaseModeChoice('external', true)).toBe('host')
    expect(databaseModeChoice('external', false)).toBe('external')
    expect(databaseModeChoice('managed', true)).toBe('managed')
  })
})

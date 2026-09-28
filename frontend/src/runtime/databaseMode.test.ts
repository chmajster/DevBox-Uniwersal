import { describe, expect, it } from 'vitest'
import { databaseModeFields } from './databaseMode'

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
      'host', 'port', 'database', 'username', 'password', 'application_host', 'application_port',
    ])
  })
})

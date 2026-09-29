import { describe, expect, it } from 'vitest'
import { databaseModeFields } from './databaseMode'

describe('project database mode field contract', () => {
  it('renders no connection fields for none mode', () => {
    expect(databaseModeFields('none')).toEqual([])
  })

  it('renders shared DevBox service details through the dedicated service selector', () => {
    expect(databaseModeFields('managed')).toEqual([])
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
})

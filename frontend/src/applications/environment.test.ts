import { describe, expect, it } from 'vitest'
import { encodeEnvironment, parseEnv } from './environment'

describe('environment editor', () => {
  it('imports quoted values, empty values, comments and secrets without executing expansion', () => {
    const rows = parseEnv('# comment\nexport APP_ENV=dev # comment\nDB_PASSWORD="a=b $HOME"\nEMPTY=\nQUOTED=\'with # hash\'')
    expect(encodeEnvironment(rows)).toEqual({ publicValues: { APP_ENV: 'dev', EMPTY: '', QUOTED: 'with # hash' }, secrets: { DB_PASSWORD: 'a=b $HOME' } })
  })
  it('rejects malformed input and duplicate names', () => {
    expect(() => parseEnv('INVALID LINE')).toThrow()
    expect(() => parseEnv('NAME="unclosed')).toThrow()
    expect(() => encodeEnvironment([{ name: 'A', value: 'one', secret: false }, { name: 'A', value: 'two', secret: true }])).toThrow()
  })
})

import { describe, expect, it } from 'vitest'
import type { RuntimeContainerConfig } from '../api/types'
import { effectiveRuntimeName, preparePHPModuleConfig, updatePHPModuleSelection } from './phpModuleConfig'

const config: RuntimeContainerConfig = {
  project_id: 'project-1',
  runtime: '',
  runtime_version: '',
  container_policy: 'auto',
  modules: [],
}

describe('PHP deployment module configuration', () => {
  it('shows PHP modules for an automatically detected PHP runtime', () => {
    expect(effectiveRuntimeName('', 'PHP', '')).toBe('php')
    expect(effectiveRuntimeName('', '', 'php')).toBe('php')
  })

  it('keeps an explicit runtime ahead of detection hints', () => {
    expect(effectiveRuntimeName('node', 'php', 'php')).toBe('node')
  })

  it('pins PHP when modules are saved from auto-detect mode', () => {
    expect(preparePHPModuleConfig(config)).toMatchObject({ runtime: 'php', container_policy: 'auto' })
  })

  it('adds and removes module selections without duplicates', () => {
    const added = updatePHPModuleSelection([{ name: 'mysqli' }], 'pdo_mysql', true)
    expect(added).toEqual([{ name: 'mysqli' }, { name: 'pdo_mysql' }])
    expect(updatePHPModuleSelection(added, 'pdo_mysql', true)).toEqual([{ name: 'mysqli' }, { name: 'pdo_mysql' }])
    expect(updatePHPModuleSelection(added, 'mysqli', false)).toEqual([{ name: 'pdo_mysql' }])
  })
})

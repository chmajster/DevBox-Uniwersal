import { describe, expect, it } from 'vitest'
import type { UpdateProgress } from '../api/types'
import { buildUpdateHardRefreshURL, clampUpdatePercent, clearUpdateHardRefreshURL, updateIsActive, updateStageState, updateStateLabel } from './progress'

function progress(change: Partial<UpdateProgress>): UpdateProgress {
  return {
    state: 'running',
    percent: 58,
    stage: 'backend',
    ...change,
  }
}

describe('update progress model', () => {
  it('clamps progress to a UI-safe percentage', () => {
    expect(clampUpdatePercent(-10)).toBe(0)
    expect(clampUpdatePercent(54.6)).toBe(55)
    expect(clampUpdatePercent(120)).toBe(100)
  })

  it('marks completed, current and pending stages', () => {
    const value = progress({})
    expect(updateStageState(value, 'download')).toBe('done')
    expect(updateStageState(value, 'backend')).toBe('current')
    expect(updateStageState(value, 'frontend')).toBe('pending')
  })

  it('marks the failing stage without completing later stages', () => {
    const value = progress({ state: 'failed', percent: 68, stage: 'frontend' })
    expect(updateStageState(value, 'backend')).toBe('done')
    expect(updateStageState(value, 'frontend')).toBe('failed')
    expect(updateStageState(value, 'artifacts')).toBe('pending')
  })

  it('shows skipped install stages when no update is required', () => {
    const value = progress({ state: 'no_update', percent: 100, stage: 'completed' })
    expect(updateStageState(value, 'download')).toBe('done')
    expect(updateStageState(value, 'backend')).toBe('skipped')
    expect(updateStageState(value, 'completed')).toBe('done')
  })

  it('labels active and terminal states', () => {
    expect(updateIsActive(progress({ state: 'starting' }))).toBe(true)
    expect(updateIsActive(progress({ state: 'succeeded' }))).toBe(false)
    expect(updateStateLabel(progress({ state: 'failed' }))).toBe('Błąd aktualizacji')
  })

  it('builds a one-time cache-busting URL and removes only the DevBox marker', () => {
    const refreshed = buildUpdateHardRefreshURL('http://127.0.0.1:8787/updates?tab=system#progress', 12345)
    expect(refreshed).toBe('http://127.0.0.1:8787/updates?tab=system&__devbox_refresh=12345#progress')
    expect(clearUpdateHardRefreshURL(refreshed)).toBe('http://127.0.0.1:8787/updates?tab=system#progress')
  })
})

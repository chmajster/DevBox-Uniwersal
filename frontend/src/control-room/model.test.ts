import { describe, expect, it } from 'vitest'
import type { Job, MonitoringSnapshot } from '../api/types'
import { appendSample, bytes, chartSegments, HISTORY_LIMIT, HISTORY_WINDOW, jobState, pendingJobs, percent, recent, time, uptime } from './model'
import { collection } from './api'

function snapshot(at: number, available = true) {
  return { collected_at: new Date(at).toISOString(), cpu: { available, usage_percent: 24 }, memory: { available: true, usage_percent: 38 }, disk: { available: true, usage_percent: 42 } } as MonitoringSnapshot
}
describe('control room telemetry', () => {
  it('distinguishes missing data, NaN and a real zero', () => {
    expect(percent(undefined)).toBeNull(); expect(percent(NaN)).toBeNull(); expect(percent(Infinity)).toBeNull()
    expect(percent(0)).toBe(0); expect(percent(-5)).toBe(0); expect(percent(110)).toBe(100)
    expect(bytes(undefined)).toBe('—'); expect(bytes(0)).toBe('0 B'); expect(bytes(1024 ** 3)).toBe('1.0 GB')
    expect(uptime(-1)).toBe('—'); expect(uptime(90000)).toBe('1 dni 1 godz.'); expect(time('invalid')).toBe('—')
  })
  it('never fabricates a time series or duplicates/out-of-order samples', () => {
    const first = appendSample([], snapshot(10000))
    expect(first).toHaveLength(1)
    expect(appendSample(first, snapshot(10000))).toBe(first)
    expect(appendSample(first, snapshot(5000))).toBe(first)
    expect(appendSample(first, { ...snapshot(10000), collected_at: 'broken' })).toBe(first)
    expect(appendSample(first, snapshot(20000, false))[1].cpu).toBeNull()
  })
  it('caps history by time and number of samples', () => {
    let history = appendSample([], snapshot(0))
    for (let i = 1; i < 800; i++) history = appendSample(history, snapshot(i * 10000))
    expect(history.length).toBeLessThanOrEqual(HISTORY_LIMIT)
    expect(history[0].at).toBeGreaterThanOrEqual(history[history.length - 1].at - HISTORY_WINDOW)
  })
  it('breaks chart lines on missing metrics and connection gaps', () => {
    let history = appendSample([], snapshot(10000))
    history = appendSample(history, snapshot(20000, false))
    history = appendSample(history, snapshot(30000))
    history = appendSample(history, snapshot(70000))
    expect(chartSegments(history, 'cpu')).toHaveLength(3)
    expect(chartSegments(history, 'memory')).toHaveLength(2)
  })
  it('normalizes actual collection envelopes but rejects invalid objects', () => {
    expect(collection(null)).toEqual([]); expect(collection({ items: [1] })).toEqual([1])
    expect(collection({ jobs: [2] })).toEqual([2]); expect(() => collection({ invalid: [] })).toThrow()
  })
  it('does not label completed jobs RUNNING and leaves unknown states neutral', () => {
    expect(jobState('succeeded').label).toBe('ZAKOŃCZONE')
    expect(jobState('unexpected').tone).toBe('muted')
    expect(pendingJobs([{ status: 'queued' }, { status: 'running' }, { status: 'succeeded' }] as Job[])).toHaveLength(2)
  })
  it('sorts newest first without changing the original collection', () => {
    const rows = [{ created_at: '2026-01-01' }, { created_at: '2026-01-02' }]
    expect(recent(rows)[0]).toBe(rows[1]); expect(rows[0].created_at).toBe('2026-01-01')
  })
})

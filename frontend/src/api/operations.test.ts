import { afterEach, describe, expect, it, vi } from 'vitest'
import { listProjects } from './operations'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('collection operations', () => {
  it('normalizes a null API collection to an empty array', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ data: null })
    } as Response))
    vi.stubGlobal('fetch', fetchMock)

    await expect(listProjects()).resolves.toEqual([])
    expect(fetchMock).toHaveBeenCalledOnce()
  })
})

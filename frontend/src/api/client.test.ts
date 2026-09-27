import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClientError, request } from './client'

describe('api client', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('unwraps successful responses', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ data: { status: 'ok' } }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await expect(request<{ status: string }>('/health')).resolves.toEqual({ status: 'ok' })
  })

  it('throws normalized api errors', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'authentication required' } }), { status: 401, headers: { 'Content-Type': 'application/json' } })))
    await expect(request('/auth/me')).rejects.toBeInstanceOf(ApiClientError)
  })
})

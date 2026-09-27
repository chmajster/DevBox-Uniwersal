import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiClientError, request } from './client'

describe('api client', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('unwraps successful responses', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ data: { status: 'ok' } }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    await expect(request<{ status: string }>('/health')).resolves.toEqual({ status: 'ok' })
  })

  it('adds the CSRF token to authenticated mutations', async () => {
    vi.stubGlobal('document', { cookie: 'devbox_csrf=csrf-test-token' })
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('csrf-test-token')
      return new Response(JSON.stringify({ data: { status: 'queued' } }), { status: 202, headers: { 'Content-Type': 'application/json' } })
    })
    vi.stubGlobal('fetch', fetchMock)
    await expect(request<{ status: string }>('/projects/project-1/deploy', { method: 'POST' })).resolves.toEqual({ status: 'queued' })
  })

  it('does not require a CSRF cookie for login', async () => {
    vi.stubGlobal('document', { cookie: '' })
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      expect(new Headers(init?.headers).get('X-CSRF-Token')).toBeNull()
      return new Response(JSON.stringify({ data: { id: 'u1' } }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    })
    vi.stubGlobal('fetch', fetchMock)
    await expect(request<{ id: string }>('/auth/login', { method: 'POST', body: '{}' })).resolves.toEqual({ id: 'u1' })
  })

  it('throws normalized api errors', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'authentication required' } }), { status: 401, headers: { 'Content-Type': 'application/json' } })))
    await expect(request('/auth/me')).rejects.toBeInstanceOf(ApiClientError)
  })
})

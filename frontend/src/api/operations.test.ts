import { afterEach, describe, expect, it, vi } from 'vitest'
import { getServiceProbe, listDatabases, listPorts, listDockerContainers } from './operations'

function apiResponse(data: unknown) {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' }
  })
}

describe('operations API integration', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('normalizes null collection payloads to empty arrays', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(apiResponse(null))
      .mockResolvedValueOnce(apiResponse(null))

    vi.stubGlobal('fetch', fetchMock)

    await expect(listDockerContainers()).resolves.toEqual([])
    await expect(listDatabases()).resolves.toEqual([])
  })

  it('uses the networking route exposed by the backend for ports', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      expect(String(input)).toBe('/api/v1/ports')
      return apiResponse([])
    })

    vi.stubGlobal('fetch', fetchMock)

    await expect(listPorts()).resolves.toEqual([])
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('uses the MySQL status endpoint and maps its native response shape', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      expect(String(input)).toBe('/api/v1/mysql/status')
      return apiResponse({
        version: '8.4.0',
        running: true,
        connection_state: 'connected'
      })
    })

    vi.stubGlobal('fetch', fetchMock)

    await expect(getServiceProbe('MySQL')).resolves.toEqual({
      name: 'MySQL',
      status: 'RUNNING',
      message: '8.4.0'
    })
  })

  it('maps Docker and Nginx provider status fields to dashboard service state', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(apiResponse({ available: true, server_version: '28.0.0' }))
      .mockResolvedValueOnce(apiResponse({ detected: true, config_valid: true, version: 'nginx/1.26.0' }))

    vi.stubGlobal('fetch', fetchMock)

    await expect(getServiceProbe('Docker')).resolves.toEqual({
      name: 'Docker',
      status: 'RUNNING',
      message: '28.0.0'
    })
    await expect(getServiceProbe('Nginx')).resolves.toEqual({
      name: 'Nginx',
      status: 'RUNNING',
      message: 'nginx/1.26.0'
    })

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/v1/docker/status',
      expect.objectContaining({ credentials: 'include' })
    )
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/v1/proxy/status',
      expect.objectContaining({ credentials: 'include' })
    )
  })
})

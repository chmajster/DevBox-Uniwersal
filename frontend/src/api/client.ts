interface Envelope<T> {
  data?: T
  meta?: Record<string, unknown>
  error?: { code: string; message: string; details?: unknown }
}

export class ApiClientError extends Error {
  constructor(public readonly code: string, message: string, public readonly status: number) {
    super(message)
  }
}

const apiBase = import.meta.env.VITE_API_BASE ?? '/api/v1'

export function apiURL(path: string) {
  return `${apiBase}${path}`
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(apiURL(path), {
    ...init,
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {})
    }
  })

  const payload = (await response.json()) as Envelope<T>
  if (!response.ok || payload.error) {
    const apiError = payload.error
    throw new ApiClientError(apiError?.code ?? 'http_error', apiError?.message ?? `HTTP ${response.status}`, response.status)
  }
  if (payload.data === undefined) {
    throw new ApiClientError('invalid_response', 'API response does not contain data', response.status)
  }
  return payload.data
}

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

function readCookie(name: string): string {
  if (typeof document === 'undefined') return ''
  const prefix = `${encodeURIComponent(name)}=`
  for (const part of document.cookie.split(';')) {
    const value = part.trim()
    if (value.startsWith(prefix)) return decodeURIComponent(value.slice(prefix.length))
  }
  return ''
}

function isMutation(method: string): boolean {
  return !['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase())
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  const formDataBody = typeof FormData !== 'undefined' && init?.body instanceof FormData
  if (!headers.has('Content-Type') && !formDataBody) headers.set('Content-Type', 'application/json')
  const method = init?.method ?? 'GET'
  if (isMutation(method) && path !== '/auth/login') {
    const csrfToken = readCookie('devbox_csrf')
    if (csrfToken) headers.set('X-CSRF-Token', csrfToken)
  }

  const response = await fetch(apiURL(path), {
    ...init,
    credentials: 'include',
    headers
  })

  const text = await response.text()
  let payload: Envelope<T> = {}
  if (text) {
    try {
      payload = JSON.parse(text) as Envelope<T>
    } catch {
      throw new ApiClientError('invalid_response', response.ok ? 'API returned invalid JSON' : `HTTP ${response.status}`, response.status)
    }
  }
  if (!response.ok || payload.error) {
    const apiError = payload.error
    throw new ApiClientError(apiError?.code ?? 'http_error', apiError?.message ?? `HTTP ${response.status}`, response.status)
  }
  if (payload.data === undefined) {
    throw new ApiClientError('invalid_response', 'API response does not contain data', response.status)
  }
  return payload.data
}

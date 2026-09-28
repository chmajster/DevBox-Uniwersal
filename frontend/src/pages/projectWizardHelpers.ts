export function repositoryNameFromURL(value: string): string {
  const input = value.trim().replace(/\/+$/, '')
  if (!input) return ''

  const scpLike = input.match(/^[^@\s]+@[^:\s]+:(.+)$/)
  const path = scpLike?.[1] ?? (() => {
    try {
      return new URL(input).pathname
    } catch {
      return input
    }
  })()

  const last = path.replace(/\\/g, '/').split('/').filter(Boolean).pop() ?? ''
  const decoded = (() => {
    try { return decodeURIComponent(last) } catch { return last }
  })()
  return decoded.replace(/\.git$/i, '').trim()
}

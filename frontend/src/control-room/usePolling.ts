import { useCallback, useEffect, useState } from 'react'

/** Serialized, abortable reads. No polling while hidden; failures invalidate stale values. */
export function usePolling<T>(load: (signal: AbortSignal) => Promise<T>, interval: number, enabled = true) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const [revision, setRevision] = useState(0)
  const refresh = useCallback(() => setRevision((value) => value + 1), [])
  useEffect(() => {
    if (!enabled) return
    let disposed = false
    let busy = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let controller: AbortController | undefined
    async function poll() {
      if (disposed || busy || document.hidden) return
      busy = true
      setLoading(true)
      controller = new AbortController()
      const signal = controller.signal
      const timeout = window.setTimeout(() => controller?.abort(), 15000)
      try {
        const value = await load(signal)
        if (!disposed) { setData(value); setError(''); setUpdatedAt(Date.now()) }
      } catch (reason) {
        if (!disposed) {
          setData(null)
          setError(signal.aborted ? 'Przekroczono czas odczytu API (15 s).' : reason instanceof Error ? reason.message : String(reason))
        }
      } finally {
        window.clearTimeout(timeout)
        busy = false
        if (!disposed) {
          setLoading(false)
          timer = window.setTimeout(() => void poll(), interval)
        }
      }
    }
    function visibility() {
      if (!document.hidden) { clearTimeout(timer); void poll() }
    }
    void poll()
    document.addEventListener('visibilitychange', visibility)
    return () => { disposed = true; clearTimeout(timer); controller?.abort(); document.removeEventListener('visibilitychange', visibility) }
  }, [load, interval, enabled, revision])
  return { data, error, loading, updatedAt, refresh }
}

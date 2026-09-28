import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { request } from '../api/client'
import type { MonitoringSnapshot } from '../api/types'
import { loadOverview } from './api'
import { appendSample, type Sample } from './model'
import { usePolling } from './usePolling'

function useController() {
  const { pathname } = useLocation()
  const [history, setHistory] = useState<Sample[]>([])
  const monitorLoader = useCallback(async (signal: AbortSignal) => {
    const snapshot = await request<MonitoringSnapshot>('/monitoring/snapshot', { signal })
    if (!snapshot || !snapshot.cpu || !snapshot.memory || !snapshot.disk || !snapshot.process) throw new Error('Nieprawidłowy odczyt monitoringu.')
    if (!signal.aborted) setHistory((current) => appendSample(current, snapshot))
    return snapshot
  }, [])
  const overview = usePolling(loadOverview, 30000)
  const monitoring = usePolling(monitorLoader, 10000, pathname === '/')
  return { overview, monitoring, history }
}
function HashTarget() {
  const { pathname, hash } = useLocation()
  useEffect(() => {
    if (!hash) return
    const frame = window.requestAnimationFrame(() => document.getElementById(hash.slice(1))?.scrollIntoView({ block: 'start' }))
    return () => window.cancelAnimationFrame(frame)
  }, [pathname, hash])
  return null
}
const Context = createContext<ReturnType<typeof useController> | null>(null)
export function ControlRoomProvider({ children }: { children: ReactNode }) { return <Context.Provider value={useController()}><HashTarget />{children}</Context.Provider> }
export function useControlRoom() {
  const value = useContext(Context)
  if (!value) throw new Error('ControlRoomProvider is required')
  return value
}

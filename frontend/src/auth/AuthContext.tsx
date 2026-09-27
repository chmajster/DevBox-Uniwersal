import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { ApiClientError, request } from '../api/client'
import type { User } from '../api/types'

interface AuthState {
  user: User | null
  loading: boolean
  login(username: string, password: string): Promise<void>
  logout(): Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    request<User>('/auth/me')
      .then(setUser)
      .catch((error: unknown) => {
        if (!(error instanceof ApiClientError) || error.status !== 401) console.error('failed to restore session', error)
      })
      .finally(() => setLoading(false))
  }, [])

  const value = useMemo<AuthState>(() => ({
    user,
    loading,
    async login(username, password) {
      const loggedIn = await request<User>('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) })
      setUser(loggedIn)
    },
    async logout() {
      await request<{ status: string }>('/auth/logout', { method: 'POST' })
      setUser(null)
    }
  }), [user, loading])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const context = useContext(AuthContext)
  if (!context) throw new Error('useAuth must be used inside AuthProvider')
  return context
}

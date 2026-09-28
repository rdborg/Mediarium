import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, type User } from './api'

interface AuthState {
  loading: boolean
  firstRunNeeded: boolean
  user: User | null
  refresh: () => Promise<void>
  setUser: (u: User | null) => void
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [firstRunNeeded, setFirstRunNeeded] = useState(false)
  const [user, setUser] = useState<User | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const status = await api.onboardingStatus()
      setFirstRunNeeded(status.firstRunNeeded)
      if (!status.firstRunNeeded) {
        try {
          const me = await api.me()
          setUser(me)
        } catch {
          setUser(null)
        }
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  return (
    <AuthContext.Provider value={{ loading, firstRunNeeded, user, refresh, setUser }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

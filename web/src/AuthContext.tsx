import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { api, ApiError, type User } from './api'

interface AuthState {
  loading: boolean
  // True while the server cannot be reached (for example it is restarting
  // after an update); the check is retried by itself until it answers.
  offline: boolean
  firstRunNeeded: boolean
  user: User | null
  // True when someone is signed in but never finished the first-run wizard
  // (for example the page was closed halfway), so it resumes.
  needsWizard: boolean
  refresh: () => Promise<void>
  setUser: (u: User | null) => void
}

const AuthContext = createContext<AuthState | null>(null)

const TIMEOUT_MS = 8000
const RETRY_MS = 3000

// Resolves like `p`, or rejects if it takes longer than TIMEOUT_MS, so a
// request left hanging by a restarting server (or a tunnel in front of it)
// cannot keep the loading screen up forever.
function withTimeout<T>(p: Promise<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error('timed out')), TIMEOUT_MS)
    p.then(
      (v) => {
        clearTimeout(t)
        resolve(v)
      },
      (e) => {
        clearTimeout(t)
        reject(e)
      },
    )
  })
}

// "Server is not there right now" as opposed to a real answer such as 401.
function unreachable(e: unknown): boolean {
  if (e instanceof ApiError) return e.status >= 500 || e.status === 0
  return true // network error or timeout
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [offline, setOffline] = useState(false)
  const [firstRunNeeded, setFirstRunNeeded] = useState(false)
  const [user, setUser] = useState<User | null>(null)
  const [needsWizard, setNeedsWizard] = useState(false)
  const retry = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const refresh = useCallback(async () => {
    clearTimeout(retry.current)
    setLoading(true)
    try {
      const status = await withTimeout(api.onboardingStatus())
      setFirstRunNeeded(status.firstRunNeeded)
      if (!status.firstRunNeeded) {
        try {
          const me = await withTimeout(api.me())
          setUser(me)
          try {
            setNeedsWizard(!(await withTimeout(api.getSettings())).onboardingDone)
          } catch {
            setNeedsWizard(false)
          }
        } catch (e) {
          if (unreachable(e)) throw e
          setUser(null) // signed out
        }
      }
      setOffline(false)
      setLoading(false)
    } catch (e) {
      if (!unreachable(e)) {
        setOffline(false)
        setLoading(false)
        return
      }
      // Keep the loading screen, say so, and try again shortly.
      setOffline(true)
      retry.current = setTimeout(() => void refresh(), RETRY_MS)
    }
  }, [])

  useEffect(() => {
    void refresh()
    return () => clearTimeout(retry.current)
  }, [refresh])

  return (
    <AuthContext.Provider value={{ loading, offline, firstRunNeeded, user, needsWizard, refresh, setUser }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

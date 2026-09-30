import { useCallback, useEffect, useState } from 'react'
import { api } from './api'
import { useLive } from './useLive'

// The page and the sidebar both show how many errors are unread. When the
// page marks some as read it says so here, and every counter looks again.
const CHANGED = 'mediarium-problems-changed'

export function problemsChanged() {
  window.dispatchEvent(new Event(CHANGED))
}

// How many errors from the last 24 hours nobody has marked as read. Zero for
// accounts that may not see the log, and while the answer is not in.
export function useUnreadErrors(enabled: boolean, ms = 30000): number {
  const [n, setN] = useState(0)
  const load = useCallback(() => {
    if (!enabled) return
    api
      .problemCounts()
      .then((c) => setN(c.unreadErrors24h))
      .catch(() => undefined)
  }, [enabled])
  useEffect(load, [load])
  useEffect(() => {
    window.addEventListener(CHANGED, load)
    return () => window.removeEventListener(CHANGED, load)
  }, [load])
  useLive(load, ms)
  return enabled ? n : 0
}

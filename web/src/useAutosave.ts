import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type Settings } from './api'
import { useToast } from './components/Toast'

// Shared behaviour for settings that apply the moment you change them (a
// switch, a checkbox): show the change immediately, save it, then trust what
// the server says it stored, not what we assumed. The value is re-read from
// the server when the page opens and whenever the browser tab comes back
// into view, so what you see is always what is really saved. A failed save
// puts the control back and says why; a successful one confirms with a toast.
export function useAutosaveSetting<T>(
  read: (s: Settings) => T,
  write: (value: T) => Parameters<typeof api.putSettings>[0],
  describe: (saved: T) => string,
  initial: T,
) {
  const [value, setValue] = useState<T>(initial)
  const [loaded, setLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const toast = useToast()
  const readRef = useRef(read)
  readRef.current = read

  const refresh = useCallback(() => {
    api
      .getSettings()
      .then((s) => {
        setValue(readRef.current(s))
        setLoaded(true)
      })
      .catch(() => undefined)
  }, [])

  useEffect(() => {
    refresh()
    const onVisible = () => {
      if (document.visibilityState === 'visible') refresh()
    }
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('focus', refresh)
    return () => {
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('focus', refresh)
    }
  }, [refresh])

  const change = useCallback(
    async (next: T) => {
      const previous = value
      setValue(next)
      setSaving(true)
      try {
        const stored = await api.putSettings(write(next))
        const confirmed = readRef.current(stored)
        setValue(confirmed)
        toast.success(describe(confirmed))
      } catch (e) {
        setValue(previous)
        toast.error(`Not saved: ${e instanceof Error ? e.message : String(e)}`)
      } finally {
        setSaving(false)
      }
    },
    [value, write, describe, toast],
  )

  return { value, change, loaded, saving, refresh }
}

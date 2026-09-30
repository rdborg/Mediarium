import { useEffect, useRef } from 'react'

// Keeps a page's data current without a reload: runs `refresh` every `ms`
// while the tab is visible, and straight away when you come back to the tab.
// It pauses while the tab is hidden so nothing polls in the background.
// Pass enabled = false to switch it off for a while (for example when nothing
// is running that needs watching).
export function useLive(refresh: () => unknown, ms: number, enabled = true) {
  const fn = useRef(refresh)
  fn.current = refresh

  useEffect(() => {
    if (!enabled) return
    let timer: ReturnType<typeof setInterval> | undefined
    const start = () => {
      if (timer === undefined) timer = setInterval(() => void fn.current(), ms)
    }
    const stop = () => {
      if (timer !== undefined) clearInterval(timer)
      timer = undefined
    }
    const onVisible = () => {
      if (document.visibilityState === 'visible') {
        void fn.current()
        start()
      } else {
        stop()
      }
    }
    if (document.visibilityState === 'visible') start()
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('focus', onVisible)
    return () => {
      stop()
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('focus', onVisible)
    }
  }, [ms, enabled])
}

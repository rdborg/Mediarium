import { useEffect, useRef } from 'react'
import { liveEvents } from './liveEvents'

// Runs `refresh` when the server says something changed ("activity"), or when
// the page may have missed something ("resync", after a hidden tab comes back
// or the stream reconnects). The stream is shared by every tab of the browser
// (see liveEvents.ts). Changes that come in a burst make one refresh.
export function useLiveEvents(refresh: () => unknown, enabled = true) {
  const fn = useRef(refresh)
  fn.current = refresh

  useEffect(() => {
    if (!enabled) return
    let timer: ReturnType<typeof setTimeout> | undefined
    const off = liveEvents((ev) => {
      if (ev.name !== 'activity' && ev.name !== 'resync') return
      if (timer !== undefined) return
      timer = setTimeout(() => {
        timer = undefined
        void fn.current()
      }, 400)
    })
    return () => {
      off()
      if (timer !== undefined) clearTimeout(timer)
    }
  }, [enabled])
}
